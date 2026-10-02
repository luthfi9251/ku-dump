package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type fakeEngine struct {
	name       string
	dumpErr    error
	restoreErr error
	block      chan struct{}
}

func (f *fakeEngine) Name() string                                               { return f.name }
func (f *fakeEngine) RequiredTools() []string                                    { return nil }
func (f *fakeEngine) ToolsMissing() []string                                     { return nil }
func (f *fakeEngine) TestConnection(ctx context.Context, db meta.Database) error { return nil }
func (f *fakeEngine) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	io.WriteString(log, "dumping "+db.DBName+"\n")
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.dumpErr != nil {
		fmt.Fprintln(log, "tool failed:", f.dumpErr)
		return f.dumpErr
	}
	io.WriteString(out, "DUMPDATA:"+db.DBName)
	return nil
}
func (f *fakeEngine) Restore(ctx context.Context, db meta.Database, sourceDB string, in io.Reader, log io.Writer) error {
	io.Copy(log, in)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.restoreErr
}

func writeFile(p, content string) error {
	return os.WriteFile(p, []byte(content), 0o644)
}

func setup(t *testing.T, eng engine.Engine) (*Runner, *meta.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := meta.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dumpsDir := filepath.Join(dir, "dumps")
	newStore := func(ctx context.Context, destID int64) (storage.Store, error) {
		if destID != 0 {
			return nil, fmt.Errorf("storage destination %d not configured", destID)
		}
		return storage.NewLocalFS(dumpsDir)
	}
	run, err := New(st, map[string]engine.Engine{"postgres": eng}, newStore, filepath.Join(dumpsDir, "_logs"))
	if err != nil {
		t.Fatal(err)
	}
	return run, st, dumpsDir
}

func seedDumpJob(t *testing.T, st *meta.Store) (dbID, dumpID, jobID int64) {
	t.Helper()
	ctx := context.Background()
	enc := "ENC"
	dbID, err := st.CreateDatabase(ctx, &meta.Database{Name: "prod", Engine: "postgres", Host: "h", Port: 1, DBName: "appdb", Username: "u", PasswordEnc: enc})
	if err != nil {
		t.Fatal(err)
	}
	dumpID, err = st.CreateDump(ctx, &meta.Dump{DatabaseID: dbID, Engine: "postgres", Label: "l", Storage: "local", SourceDB: "appdb", Status: "pending", CreatedBy: 1})
	if err != nil {
		t.Fatal(err)
	}
	jobID, err = st.CreateJob(ctx, &meta.Job{Type: "dump", DatabaseID: dbID, DumpID: &dumpID})
	if err != nil {
		t.Fatal(err)
	}
	return dbID, dumpID, jobID
}

func waitTerminal(t *testing.T, st *meta.Store, jobID int64) meta.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err := st.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		switch j.Status {
		case "success", "failed", "cancelled":
			return *j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish in time")
	return meta.Job{}
}

func TestDumpJobSuccess(t *testing.T) {
	run, st, dumpsDir := setup(t, &fakeEngine{name: "postgres"})
	_, dumpID, jobID := seedDumpJob(t, st)
	run.Start(jobID)
	j := waitTerminal(t, st, jobID)
	if j.Status != "success" {
		t.Fatalf("status = %s, err = %s", j.Status, j.Err)
	}
	d, _ := st.GetDump(context.Background(), dumpID)
	if d.Status != "ready" || d.SizeBytes == 0 || d.Location == "" {
		t.Fatalf("dump = %+v", d)
	}
	if !strings.HasPrefix(d.Location, "postgres/prod/") || !strings.HasSuffix(d.Location, ".dump") {
		t.Fatalf("location = %q", d.Location)
	}
	local, err := storage.NewLocalFS(dumpsDir)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := local.Open(context.Background(), d.Location)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "DUMPDATA:appdb" {
		t.Fatalf("stored = %q", b)
	}
}

func TestDumpJobWritesProgressLog(t *testing.T) {
	run, st, _ := setup(t, &fakeEngine{name: "postgres"})
	_, _, jobID := seedDumpJob(t, st)
	run.Start(jobID)
	j := waitTerminal(t, st, jobID)
	if j.Status != "success" {
		t.Fatalf("status = %s", j.Status)
	}
	log := TailFile(j.LogPath, 65536)
	for _, want := range []string{"starting dump", "dump finished", "uploading to local", "stored as"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing %q:\n%s", want, log)
		}
	}
}

func TestRestoreJobWritesProgressLog(t *testing.T) {
	run, st, _ := setup(t, &fakeEngine{name: "postgres"})
	_, dumpID, jobID := seedDumpJob(t, st)
	run.Start(jobID)
	waitTerminal(t, st, jobID)
	ctx := context.Background()
	targetID, _ := st.CreateDatabase(ctx, &meta.Database{Name: "dev", Engine: "postgres", Host: "h", Port: 1, DBName: "devdb", Username: "u", PasswordEnc: "ENC"})
	restoreJob, _ := st.CreateJob(ctx, &meta.Job{Type: "restore", DatabaseID: targetID, DumpID: &dumpID})
	run.Start(restoreJob)
	j := waitTerminal(t, st, restoreJob)
	if j.Status != "success" {
		t.Fatalf("status = %s err = %s", j.Status, j.Err)
	}
	log := TailFile(j.LogPath, 65536)
	for _, want := range []string{"starting restore", "restore finished"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing %q:\n%s", want, log)
		}
	}
}

func TestDumpJobUsesWorkflowStorageConfig(t *testing.T) {
	run, st, dumpsDir := setup(t, &fakeEngine{name: "postgres"})
	ctx := context.Background()
	dbID, err := st.CreateDatabase(ctx, &meta.Database{Name: "prod", Engine: "postgres", Host: "h", Port: 1, DBName: "appdb", Username: "u", PasswordEnc: "ENC"})
	if err != nil {
		t.Fatal(err)
	}
	wfID, err := st.CreateWorkflow(ctx, &meta.Workflow{Name: "Nightly Backup", DatabaseID: dbID,
		TriggerKind: "manual", Enabled: true, StoragePath: "backups/prod", FilenamePattern: "{db}-{date}"})
	if err != nil {
		t.Fatal(err)
	}
	dumpID, err := st.CreateDump(ctx, &meta.Dump{DatabaseID: dbID, Engine: "postgres", Label: "l",
		Storage: "local", SourceDB: "appdb", Status: "pending", CreatedBy: 1, WorkflowID: wfID})
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := st.CreateJob(ctx, &meta.Job{Type: "dump", DatabaseID: dbID, DumpID: &dumpID})
	if err != nil {
		t.Fatal(err)
	}
	run.Start(jobID)
	j := waitTerminal(t, st, jobID)
	if j.Status != "success" {
		t.Fatalf("status = %s err = %s", j.Status, j.Err)
	}
	d, _ := st.GetDump(ctx, dumpID)
	if !strings.HasPrefix(d.Location, "backups/prod/prod-") || !strings.HasSuffix(d.Location, ".dump") {
		t.Fatalf("location = %q", d.Location)
	}
	local, err := storage.NewLocalFS(dumpsDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Open(ctx, d.Location); err != nil {
		t.Fatalf("stored file missing: %v", err)
	}
}

func TestStartIsIdempotent(t *testing.T) {
	run, st, _ := setup(t, &fakeEngine{name: "postgres"})
	_, _, jobID := seedDumpJob(t, st)
	run.Start(jobID)
	run.Start(jobID)
	j := waitTerminal(t, st, jobID)
	if j.Status != "success" {
		t.Fatalf("status = %s, err = %s", j.Status, j.Err)
	}
}

func TestDumpJobFailure(t *testing.T) {
	run, st, _ := setup(t, &fakeEngine{name: "postgres", dumpErr: errors.New("boom")})
	_, dumpID, jobID := seedDumpJob(t, st)
	run.Start(jobID)
	j := waitTerminal(t, st, jobID)
	if j.Status != "failed" {
		t.Fatalf("status = %s", j.Status)
	}
	if !strings.Contains(j.Err, "boom") || !strings.Contains(j.Err, "tool failed") {
		t.Fatalf("err = %q", j.Err)
	}
	d, _ := st.GetDump(context.Background(), dumpID)
	if d.Status != "failed" {
		t.Fatalf("dump status = %s", d.Status)
	}
}

func TestDumpJobCancel(t *testing.T) {
	block := make(chan struct{})
	run, st, _ := setup(t, &fakeEngine{name: "postgres", block: block})
	_, _, jobID := seedDumpJob(t, st)
	run.Start(jobID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := st.GetJob(context.Background(), jobID)
		if j.Status == "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !run.Cancel(jobID) {
		t.Fatal("cancel returned false")
	}
	j := waitTerminal(t, st, jobID)
	if j.Status != "cancelled" {
		t.Fatalf("status = %s", j.Status)
	}
	close(block)
}

func TestRestoreJob(t *testing.T) {
	run, st, _ := setup(t, &fakeEngine{name: "postgres"})
	dbID, dumpID, jobID := seedDumpJob(t, st)
	run.Start(jobID)
	waitTerminal(t, st, jobID)
	ctx := context.Background()
	targetID, _ := st.CreateDatabase(ctx, &meta.Database{Name: "dev", Engine: "postgres", Host: "h", Port: 1, DBName: "devdb", Username: "u", PasswordEnc: "ENC"})
	restoreJob, _ := st.CreateJob(ctx, &meta.Job{Type: "restore", DatabaseID: targetID, DumpID: &dumpID})
	run.Start(restoreJob)
	j := waitTerminal(t, st, restoreJob)
	if j.Status != "success" {
		t.Fatalf("status = %s err = %s", j.Status, j.Err)
	}
	got, _ := st.GetDump(ctx, dumpID)
	if got.DatabaseID != dbID {
		t.Fatalf("dump row changed: %+v", got)
	}
}

func TestDumpJobFailureAfterDumpMarksDumpFailed(t *testing.T) {
	run, st, _ := setup(t, &fakeEngine{name: "postgres"})
	ctx := context.Background()
	dbID, err := st.CreateDatabase(ctx, &meta.Database{Name: "prod", Engine: "postgres", Host: "h", Port: 1, DBName: "appdb", Username: "u", PasswordEnc: "ENC"})
	if err != nil {
		t.Fatal(err)
	}
	dumpID, err := st.CreateDump(ctx, &meta.Dump{DatabaseID: dbID, Engine: "postgres", Label: "l", Storage: "s3", DestID: 99, SourceDB: "appdb", Status: "pending", CreatedBy: 1})
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := st.CreateJob(ctx, &meta.Job{Type: "dump", DatabaseID: dbID, DumpID: &dumpID})
	if err != nil {
		t.Fatal(err)
	}
	run.Start(jobID)
	j := waitTerminal(t, st, jobID)
	if j.Status != "failed" {
		t.Fatalf("status = %s", j.Status)
	}
	d, _ := st.GetDump(ctx, dumpID)
	if d.Status != "failed" || d.Location != "" || d.SizeBytes != 0 {
		t.Fatalf("dump = %+v", d)
	}
}

func TestRecoverFailsActiveJobs(t *testing.T) {
	run, st, _ := setup(t, &fakeEngine{name: "postgres"})
	_, dumpID, jobID := seedDumpJob(t, st)
	if err := st.SetJobRunning(context.Background(), jobID, ""); err != nil {
		t.Fatal(err)
	}
	if err := run.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	j, _ := st.GetJob(context.Background(), jobID)
	if j.Status != "failed" || !strings.Contains(j.Err, "interrupted") {
		t.Fatalf("job = %+v", j)
	}
	d, _ := st.GetDump(context.Background(), dumpID)
	if d.Status != "failed" {
		t.Fatalf("dump status = %s, want failed after recover", d.Status)
	}
}

func TestTails(t *testing.T) {
	if got := TailString("a\nb\nc\n", 2); got != "b\nc" {
		t.Fatalf("TailString = %q", got)
	}
	p := filepath.Join(t.TempDir(), "log")
	long := strings.Repeat("x", 10000)
	if err := writeFile(p, long); err != nil {
		t.Fatal(err)
	}
	got := TailFile(p, 100)
	if len(got) != 100 || !strings.HasPrefix(got, "x") {
		t.Fatalf("TailFile len = %d", len(got))
	}
}
