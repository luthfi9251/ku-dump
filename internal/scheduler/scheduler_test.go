package scheduler

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type instantEngine struct{}

func (instantEngine) Name() string            { return "postgres" }
func (instantEngine) RequiredTools() []string { return nil }
func (instantEngine) ToolsMissing() []string  { return nil }
func (instantEngine) TestConnection(context.Context, meta.Database) error {
	return nil
}
func (instantEngine) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	return nil
}
func (instantEngine) Restore(context.Context, meta.Database, string, io.Reader, io.Writer) error {
	return nil
}

type fixture struct {
	sched *Scheduler
	st    *meta.Store
	dbID  int64
	calls *int
	fail  *error
}

func newFixture(t *testing.T) fixture {
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
	run, err := runner.New(st, map[string]engine.Engine{"postgres": instantEngine{}},
		func(_ context.Context, destID int64) (storage.Store, error) {
			return storage.NewLocalFS(t.TempDir())
		}, filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{st: st, dbID: dbID, calls: new(int), fail: new(error)}
	// wrapper to count StartDump calls and inject failures
	sched := New(st, countingRunner{run, f.calls, f.fail})
	f.sched = sched
	return f
}

type countingRunner struct {
	*runner.Runner
	calls *int
	fail  *error
}

func (c countingRunner) StartDump(ctx context.Context, databaseID, destID int64,
	label string, workflowID, createdBy int64) (int64, int64, error) {
	*c.calls++
	if *c.fail != nil {
		return 0, 0, *c.fail
	}
	return c.Runner.StartDump(ctx, databaseID, destID, label, workflowID, createdBy)
}

func createWf(t *testing.T, f fixture, mut func(*meta.Workflow)) meta.Workflow {
	t.Helper()
	now := time.Now()
	wf := meta.Workflow{Name: "w", DatabaseID: f.dbID, TriggerKind: "cron",
		Cron: "* * * * *", Enabled: true, NextRunAt: &now}
	mut(&wf)
	id, err := f.st.CreateWorkflow(context.Background(), &wf)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := f.st.GetWorkflow(context.Background(), id)
	return *out
}

func TestRunOnceCronAdvances(t *testing.T) {
	f := newFixture(t)
	now := time.Now().Add(-time.Hour)
	wf := createWf(t, f, func(w *meta.Workflow) { w.NextRunAt = &now })
	f.sched.RunOnce(context.Background(), time.Now())

	got, _ := f.st.GetWorkflow(context.Background(), wf.ID)
	sched, _ := cron.ParseStandard(wf.Cron)
	want := sched.Next(time.Now())
	if *f.calls != 1 || got.LastRunAt == nil || got.LastError != "" || !got.Enabled ||
		got.NextRunAt == nil || got.NextRunAt.Unix() != want.Unix() {
		t.Fatalf("calls=%d wf=%+v wantNext=%v", *f.calls, got, want)
	}
}

func TestRunOnceOnceDisables(t *testing.T) {
	f := newFixture(t)
	past := time.Now().Add(-time.Hour)
	wf := createWf(t, f, func(w *meta.Workflow) {
		w.TriggerKind = "once"
		w.Cron = ""
		w.RunAt = &past
		w.NextRunAt = &past
	})
	f.sched.RunOnce(context.Background(), time.Now())

	got, _ := f.st.GetWorkflow(context.Background(), wf.ID)
	if *f.calls != 1 || got.Enabled || got.NextRunAt != nil || got.LastError != "" {
		t.Fatalf("calls=%d wf=%+v", *f.calls, got)
	}
}

func TestRunOnceStorageFailureRetries(t *testing.T) {
	f := newFixture(t)
	*f.fail = runner.ErrStorageUnavailable
	past := time.Now().Add(-time.Hour)
	wf := createWf(t, f, func(w *meta.Workflow) { w.NextRunAt = &past })
	f.sched.RunOnce(context.Background(), time.Now())

	got, _ := f.st.GetWorkflow(context.Background(), wf.ID)
	if *f.calls != 1 || !got.Enabled || got.LastError == "" ||
		got.NextRunAt == nil || got.NextRunAt.Unix() != past.Unix() {
		t.Fatalf("retry semantics broken: calls=%d wf=%+v", *f.calls, got)
	}
}

func TestRunOnceBusyAdvances(t *testing.T) {
	f := newFixture(t)
	*f.fail = errors.New("a job is already active for this database")
	past := time.Now().Add(-time.Hour)
	wf := createWf(t, f, func(w *meta.Workflow) { w.NextRunAt = &past })
	f.sched.RunOnce(context.Background(), time.Now())

	got, _ := f.st.GetWorkflow(context.Background(), wf.ID)
	sched, _ := cron.ParseStandard(wf.Cron)
	want := sched.Next(time.Now())
	if *f.calls != 1 || !got.Enabled || got.LastError == "" ||
		got.NextRunAt == nil || got.NextRunAt.Unix() != want.Unix() {
		t.Fatalf("busy must advance schedule: calls=%d wf=%+v", *f.calls, got)
	}
}

func TestRecover(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now()

	// cron with stale next_run_at -> recomputed
	stale := now.Add(-2 * time.Hour)
	wf1 := createWf(t, f, func(w *meta.Workflow) { w.Name = "cron"; w.NextRunAt = &stale })
	// once in the past, still enabled -> missed
	wf2 := createWf(t, f, func(w *meta.Workflow) {
		w.Name = "once"
		w.TriggerKind = "once"
		w.Cron = ""
		w.RunAt = &stale
		w.NextRunAt = &stale
	})
	// once in the future -> untouched
	future := now.Add(time.Hour)
	wf3 := createWf(t, f, func(w *meta.Workflow) {
		w.Name = "once-future"
		w.TriggerKind = "once"
		w.Cron = ""
		w.RunAt = &future
		w.NextRunAt = &future
	})

	if err := f.sched.Recover(ctx, now); err != nil {
		t.Fatal(err)
	}
	g1, _ := f.st.GetWorkflow(ctx, wf1.ID)
	sched, _ := cron.ParseStandard(g1.Cron)
	want := sched.Next(now)
	if g1.NextRunAt == nil || g1.NextRunAt.Unix() != want.Unix() {
		t.Fatalf("cron not recomputed: %+v", g1)
	}
	g2, _ := f.st.GetWorkflow(ctx, wf2.ID)
	if g2.Enabled || g2.NextRunAt != nil || g2.LastError != "missed" {
		t.Fatalf("once not marked missed: %+v", g2)
	}
	g3, _ := f.st.GetWorkflow(ctx, wf3.ID)
	if !g3.Enabled || g3.NextRunAt == nil || g3.NextRunAt.Unix() != future.Unix() {
		t.Fatalf("future once disturbed: %+v", g3)
	}
	if *f.calls != 0 {
		t.Fatalf("recover must not run anything, calls=%d", *f.calls)
	}
}

func TestRunOnceRecoversFromPanic(t *testing.T) {
	f := newFixture(t)
	*f.fail = nil
	past := time.Now().Add(-time.Hour)
	createWf(t, f, func(w *meta.Workflow) { w.NextRunAt = &past })
	// panic inside StartDump must not escape RunOnce
	sched := New(f.st, panickyRunner{})
	sched.RunOnce(context.Background(), time.Now())
}

type panickyRunner struct{}

func (panickyRunner) StartDump(context.Context, int64, int64, string, int64, int64) (int64, int64, error) {
	panic("boom")
}
