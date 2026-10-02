package runner

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type okEngine struct{ cx *cryptx.Cryptx }

func (okEngine) Name() string                                        { return "postgres" }
func (okEngine) RequiredTools() []string                             { return nil }
func (okEngine) ToolsMissing() []string                              { return nil }
func (okEngine) TestConnection(context.Context, meta.Database) error { return nil }
func (okEngine) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	_, err := out.Write([]byte("PGDMP-fake"))
	return err
}
func (okEngine) Restore(context.Context, meta.Database, string, io.Reader, io.Writer) error {
	return nil
}

type missingEngine struct{ okEngine }

func (missingEngine) ToolsMissing() []string { return []string{"pg_dump"} }

type blockedEngine struct{ okEngine }

func (blockedEngine) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	<-ctx.Done()
	return ctx.Err()
}

func newStartDumpRunner(t *testing.T, eng engine.Engine, newStore func(context.Context, int64) (storage.Store, error)) (*Runner, *meta.Store, int64) {
	t.Helper()
	st, err := meta.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	dbID, err := st.CreateDatabase(ctx, &meta.Database{Name: "db1", Engine: "postgres",
		Host: "h", Port: 1, DBName: "db1", Username: "u", PasswordEnc: "x"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := New(st, map[string]engine.Engine{"postgres": eng}, newStore, filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatal(err)
	}
	return run, st, dbID
}

func waitDumpStatus(t *testing.T, st *meta.Store, dumpID int64, want string) meta.Dump {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		d, err := st.GetDump(context.Background(), dumpID)
		if err == nil && d.Status == want {
			return *d
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("dump %d never became %s", dumpID, want)
	return meta.Dump{}
}

func TestStartDumpRuns(t *testing.T) {
	run, st, dbID := newStartDumpRunner(t, okEngine{}, func(_ context.Context, destID int64) (storage.Store, error) {
		return storage.NewLocalFS(t.TempDir())
	})
	ctx := context.Background()
	jobID, dumpID, err := run.StartDump(ctx, dbID, 0, "lbl", 42, 3)
	if err != nil {
		t.Fatal(err)
	}
	d := waitDumpStatus(t, st, dumpID, "ready")
	if d.WorkflowID != 42 || d.Label != "lbl" || d.Storage != "local" || d.CreatedBy != 3 {
		t.Fatalf("dump = %+v", d)
	}
	job, err := st.GetJob(ctx, jobID)
	if err != nil || job.Type != "dump" {
		t.Fatalf("job = %+v, %v", job, err)
	}
}

func TestStartDumpDefaultLabel(t *testing.T) {
	run, st, dbID := newStartDumpRunner(t, okEngine{}, func(_ context.Context, destID int64) (storage.Store, error) {
		return storage.NewLocalFS(t.TempDir())
	})
	_, dumpID, err := run.StartDump(context.Background(), dbID, 0, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	d := waitDumpStatus(t, st, dumpID, "ready")
	if d.Label == "" {
		t.Fatal("default label not applied")
	}
}

func TestStartDumpJobActive(t *testing.T) {
	// engine never finishes => first job stays running => guard triggers
	blocked := blockedEngine{}
	run, st, dbID := newStartDumpRunner(t, blocked, func(_ context.Context, destID int64) (storage.Store, error) {
		return storage.NewLocalFS(t.TempDir())
	})
	ctx := context.Background()
	_, dumpID, err := run.StartDump(ctx, dbID, 0, "first", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = run.StartDump(ctx, dbID, 0, "second", 0, 0)
	if err != meta.ErrJobActive {
		t.Fatalf("err = %v", err)
	}
	// the rejected run must not leave a pending dump row behind
	if _, err := st.GetDump(ctx, dumpID+1); err != meta.ErrNotFound {
		t.Fatalf("orphan dump row: %v", err)
	}
}

func TestStartDumpToolsMissing(t *testing.T) {
	run, _, dbID := newStartDumpRunner(t, missingEngine{}, nil)
	_, _, err := run.StartDump(context.Background(), dbID, 0, "", 0, 0)
	if !errors.Is(err, ErrToolsMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestStartDumpStorageUnavailable(t *testing.T) {
	run, _, dbID := newStartDumpRunner(t, okEngine{}, func(_ context.Context, destID int64) (storage.Store, error) {
		return nil, context.DeadlineExceeded
	})
	_, _, err := run.StartDump(context.Background(), dbID, 0, "", 0, 0)
	if !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
