package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type Runner struct {
	st       *meta.Store
	engines  map[string]engine.Engine
	newStore func(ctx context.Context, destID int64) (storage.Store, error)
	logsDir  string
	mu       sync.Mutex
	cancels  map[int64]context.CancelFunc
	started  map[int64]bool
}

func New(st *meta.Store, engines map[string]engine.Engine,
	newStore func(ctx context.Context, destID int64) (storage.Store, error), logsDir string) (*Runner, error) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, err
	}
	return &Runner{st: st, engines: engines, newStore: newStore, logsDir: logsDir,
		cancels: map[int64]context.CancelFunc{}, started: map[int64]bool{}}, nil
}

func (r *Runner) Recover(ctx context.Context) error {
	if _, err := r.st.FailActiveJobs(ctx, "interrupted by restart"); err != nil {
		return err
	}
	return r.st.FailPendingDumps(ctx)
}

func (r *Runner) Start(jobID int64) {
	r.mu.Lock()
	if r.started[jobID] {
		r.mu.Unlock()
		return
	}
	r.started[jobID] = true
	r.mu.Unlock()
	go r.run(jobID)
}

func (r *Runner) Cancel(jobID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.cancels[jobID]
	if !ok {
		return false
	}
	cancel()
	return true
}

func (r *Runner) register(id int64, c context.CancelFunc) {
	r.mu.Lock()
	r.cancels[id] = c
	r.mu.Unlock()
}

func (r *Runner) unregister(id int64) {
	r.mu.Lock()
	delete(r.cancels, id)
	r.mu.Unlock()
}

func (r *Runner) run(jobID int64) {
	ctx := context.Background()
	job, err := r.st.GetJob(ctx, jobID)
	if err != nil {
		return
	}
	if job.DumpID == nil {
		_ = r.st.SetJobFinished(ctx, jobID, "failed", "job has no dump reference")
		return
	}
	db, err := r.st.GetDatabase(ctx, job.DatabaseID)
	if err != nil {
		_ = r.st.SetJobFinished(ctx, jobID, "failed", "database not found")
		return
	}
	eng := r.engines[db.Engine]
	if eng == nil {
		_ = r.st.SetJobFinished(ctx, jobID, "failed", "unknown engine: "+db.Engine)
		return
	}
	logPath := filepath.Join(r.logsDir, fmt.Sprintf("job-%d.log", jobID))
	lf, err := os.Create(logPath)
	if err != nil {
		_ = r.st.SetJobFinished(ctx, jobID, "failed", "cannot create log file: "+err.Error())
		return
	}
	defer lf.Close()
	if err := r.st.SetJobRunning(ctx, jobID, logPath); err != nil {
		return
	}
	jctx, cancel := context.WithCancel(context.Background())
	r.register(jobID, cancel)
	defer func() {
		cancel()
		r.unregister(jobID)
	}()

	var runErr error
	switch job.Type {
	case "dump":
		runErr = r.runDump(jctx, job, db, eng, lf)
	case "restore":
		runErr = r.runRestore(jctx, job, db, eng, lf)
	default:
		runErr = fmt.Errorf("unknown job type %q", job.Type)
	}

	status, errMsg := "success", ""
	if runErr != nil {
		status = "failed"
		errMsg = TailString(runErr.Error()+"\n"+TailFile(logPath, 8192), 50)
		if jctx.Err() != nil {
			status = "cancelled"
			errMsg = "cancelled by user"
		}
	}
	_ = r.st.SetJobFinished(context.Background(), jobID, status, errMsg)
}

// logf writes a timestamped progress line into the job log. CLI tools are
// silent on success (pg_dump/pg_restore write stderr only on warnings and
// errors), so the runner emits its own progress markers to make logs useful.
func logf(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "[%s] %s\n", time.Now().UTC().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}

func (r *Runner) runDump(ctx context.Context, job *meta.Job, db *meta.Database, eng engine.Engine, log io.Writer) (err error) {
	dump, err := r.st.GetDump(ctx, *job.DumpID)
	if err != nil {
		return err
	}
	started := time.Now()
	logf(log, "starting dump of %s (%s %s:%d/%s) to %s",
		db.Name, db.Engine, db.Host, db.Port, db.DBName, dump.Storage)
	tmp, err := os.CreateTemp("", "kudump-*.dump")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	defer func() {
		if err != nil {
			_ = r.st.UpdateDumpResult(context.Background(), *job.DumpID, "failed", "", 0)
		}
	}()
	if err = eng.Dump(ctx, *db, tmp, log); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	store, err := r.newStore(ctx, dump.DestID)
	if err != nil {
		return err
	}
	var info os.FileInfo
	if info, err = os.Stat(tmpName); err != nil {
		return err
	}
	logf(log, "dump finished: %d bytes in %s", info.Size(), time.Since(started).Round(time.Millisecond))
	key := r.dumpKey(ctx, dump, db)
	logf(log, "uploading to %s as %s", dump.Storage, key)
	if err = store.Put(ctx, key, tmpName); err != nil {
		return err
	}
	logf(log, "stored as %s — dump is now available in the Dumps page", key)
	err = r.st.UpdateDumpResult(ctx, *job.DumpID, "ready", key, info.Size())
	return err
}

// dumpKey resolves the storage key for a dump. Dumps produced by a workflow
// use the workflow's folder + filename pattern; everything else (uploads,
// legacy rows) falls back to the default engine/db/timestamp layout.
func (r *Runner) dumpKey(ctx context.Context, dump *meta.Dump, db *meta.Database) string {
	if dump.WorkflowID != 0 {
		if wf, err := r.st.GetWorkflow(ctx, dump.WorkflowID); err == nil {
			return storage.BuildKey(wf.StoragePath, wf.FilenamePattern, db.Engine, db.Name, wf.Name, time.Now().UTC())
		}
	}
	return storage.KeyFor(db.Engine, storage.Slug(db.Name), time.Now().UTC())
}

func (r *Runner) runRestore(ctx context.Context, job *meta.Job, db *meta.Database, eng engine.Engine, log io.Writer) error {
	dump, err := r.st.GetDump(ctx, *job.DumpID)
	if err != nil {
		return err
	}
	started := time.Now()
	logf(log, "starting restore of %q into %s (%s %s:%d/%s)", dump.Label, db.Name, db.Engine, db.Host, db.Port, db.DBName)
	store, err := r.newStore(ctx, dump.DestID)
	if err != nil {
		return err
	}
	rc, err := store.Open(ctx, dump.Location)
	if err != nil {
		return err
	}
	defer rc.Close()
	logf(log, "reading dump %s from %s", dump.Location, dump.Storage)
	if err := eng.Restore(ctx, *db, dump.SourceDB, rc, log); err != nil {
		return err
	}
	logf(log, "restore finished in %s", time.Since(started).Round(time.Millisecond))
	return nil
}

func TailFile(path string, maxBytes int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	size := info.Size()
	if size > maxBytes {
		if _, err := f.Seek(size-maxBytes, 0); err != nil {
			return ""
		}
		size = maxBytes
	}
	if size <= 0 {
		return ""
	}
	buf := make([]byte, size)
	n, _ := f.Read(buf)
	return string(buf[:n])
}

func TailString(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

var ErrToolsMissing = errors.New("missing tools")
var ErrStorageUnavailable = errors.New("storage unavailable")

// StartDump is the single path for creating a dump run: validate engine,
// tools and storage, insert the dump row, create the guarded job, start it.
func (r *Runner) StartDump(ctx context.Context, databaseID, destID int64,
	label string, workflowID, createdBy int64) (int64, int64, error) {
	db, err := r.st.GetDatabase(ctx, databaseID)
	if err != nil {
		return 0, 0, fmt.Errorf("database not found: %w", err)
	}
	eng := r.engines[db.Engine]
	if eng == nil {
		return 0, 0, fmt.Errorf("unknown engine: %s", db.Engine)
	}
	if missing := eng.ToolsMissing(); len(missing) > 0 {
		return 0, 0, fmt.Errorf("%w: %s", ErrToolsMissing, strings.Join(missing, ", "))
	}
	storageKind := "local"
	if destID != 0 {
		dest, err := r.st.GetDestination(ctx, destID)
		if err != nil {
			return 0, 0, fmt.Errorf("storage destination not found")
		}
		storageKind = dest.Kind
	}
	if _, err := r.newStore(ctx, destID); err != nil {
		return 0, 0, fmt.Errorf("%w: %s", ErrStorageUnavailable, err)
	}
	if label == "" {
		label = db.Name + " " + time.Now().Format("2006-01-02 15:04")
	}
	dumpID, err := r.st.CreateDump(ctx, &meta.Dump{
		DatabaseID: db.ID, Engine: db.Engine, Label: label, Storage: storageKind,
		DestID: destID, SourceDB: db.DBName, Status: "pending", CreatedBy: createdBy,
		WorkflowID: workflowID,
	})
	if err != nil {
		return 0, 0, err
	}
	jobID, err := r.st.CreateJobGuarded(ctx, &meta.Job{Type: "dump", DatabaseID: db.ID, DumpID: &dumpID})
	if err != nil {
		if errors.Is(err, meta.ErrJobActive) {
			_ = r.st.DeleteDump(ctx, dumpID)
		}
		return 0, 0, err
	}
	r.Start(jobID)
	return jobID, dumpID, nil
}
