package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

// multiNewStore mirrors the main.go factory: destID 0 = dumps dir, rows are
// resolved per kind.
func multiNewStore(st *meta.Store, dumpsDir string) func(context.Context, int64) (storage.Store, error) {
	return func(ctx context.Context, destID int64) (storage.Store, error) {
		if destID == 0 {
			return storage.NewLocalFS(dumpsDir)
		}
		d, err := st.GetDestination(ctx, destID)
		if err != nil {
			return nil, err
		}
		switch d.Kind {
		case "local":
			return storage.NewLocalFS(d.RootPath)
		default:
			return nil, context.Canceled // not under test here
		}
	}
}

func TestDumpAndRestoreViaLocalDestinationRow(t *testing.T) {
	dir := t.TempDir()
	st, err := meta.Open(filepath.Join(dir, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()

	enc := "ENC"
	dbID, err := st.CreateDatabase(ctx, &meta.Database{Name: "prod", Engine: "postgres",
		Host: "h", Port: 1, DBName: "appdb", Username: "u", PasswordEnc: enc})
	if err != nil {
		t.Fatal(err)
	}
	destRoot := t.TempDir()
	destID, err := st.CreateDestination(ctx, &meta.Destination{Name: "nas", Kind: "local", RootPath: destRoot})
	if err != nil {
		t.Fatal(err)
	}

	run, err := New(st, map[string]engine.Engine{"postgres": okEngine{}}, multiNewStore(st, filepath.Join(dir, "dumps")), filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	jobID, dumpID, err := run.StartDump(ctx, dbID, destID, "lbl", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	j := waitTerminal(t, st, jobID)
	if j.Status != "success" {
		t.Fatalf("dump status = %s err = %s", j.Status, j.Err)
	}
	d, _ := st.GetDump(ctx, dumpID)
	if d.Storage != "local" || d.DestID != destID {
		t.Fatalf("dump = %+v", d)
	}
	if !strings.HasPrefix(d.Location, "postgres/prod/") {
		t.Fatalf("location = %q", d.Location)
	}
	if _, err := os.Stat(filepath.Join(destRoot, filepath.FromSlash(d.Location))); err != nil {
		t.Fatalf("file not in destination root: %v", err)
	}

	// restore from the explicit local row
	targetID, err := st.CreateDatabase(ctx, &meta.Database{Name: "dev", Engine: "postgres",
		Host: "h", Port: 1, DBName: "devdb", Username: "u", PasswordEnc: enc})
	if err != nil {
		t.Fatal(err)
	}
	rJob, err := st.CreateJob(ctx, &meta.Job{Type: "restore", DatabaseID: targetID, DumpID: &dumpID})
	if err != nil {
		t.Fatal(err)
	}
	run.Start(rJob)
	rj := waitTerminal(t, st, rJob)
	if rj.Status != "success" {
		t.Fatalf("restore status = %s err = %s", rj.Status, rj.Err)
	}
}
