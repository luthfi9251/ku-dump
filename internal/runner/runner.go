package runner

import (
	"context"
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
	newStore func(kind string) (storage.Store, error)
	logsDir  string
	mu       sync.Mutex
	cancels  map[int64]context.CancelFunc
	started  map[int64]bool
}

func New(st *meta.Store, engines map[string]engine.Engine,
	newStore func(kind string) (storage.Store, error), logsDir string) (*Runner, error) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, err
	}
	return &Runner{st: st, engines: engines, newStore: newStore, logsDir: logsDir,
		cancels: map[int64]context.CancelFunc{}, started: map[int64]bool{}}, nil
}

func (r *Runner) Recover(ctx context.Context) error {
	_, err := r.st.FailActiveJobs(ctx, "interrupted by restart")
	return err
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

func (r *Runner) runDump(ctx context.Context, job *meta.Job, db *meta.Database, eng engine.Engine, log io.Writer) error {
	tmp, err := os.CreateTemp("", "kudump-*.dump")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	dumpErr := eng.Dump(ctx, *db, tmp, log)
	closeErr := tmp.Close()
	if dumpErr != nil {
		_ = r.st.UpdateDumpResult(context.Background(), *job.DumpID, "failed", "", 0)
		return dumpErr
	}
	if closeErr != nil {
		return closeErr
	}
	store, err := r.storeFor(job.Storage)
	if err != nil {
		return err
	}
	info, err := os.Stat(tmpName)
	if err != nil {
		return err
	}
	key := storage.KeyFor(db.Engine, storage.Slug(db.Name), time.Now().UTC())
	if err := store.Put(ctx, key, tmpName); err != nil {
		return err
	}
	return r.st.UpdateDumpResult(ctx, *job.DumpID, "ready", key, info.Size())
}

func (r *Runner) runRestore(ctx context.Context, job *meta.Job, db *meta.Database, eng engine.Engine, log io.Writer) error {
	dump, err := r.st.GetDump(ctx, *job.DumpID)
	if err != nil {
		return err
	}
	store, err := r.storeFor(&dump.Storage)
	if err != nil {
		return err
	}
	rc, err := store.Open(ctx, dump.Location)
	if err != nil {
		return err
	}
	defer rc.Close()
	return eng.Restore(ctx, *db, dump.SourceDB, rc, log)
}

func (r *Runner) storeFor(kind *string) (storage.Store, error) {
	if kind == nil || *kind == "" {
		return nil, fmt.Errorf("no storage selected for job")
	}
	return r.newStore(*kind)
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
