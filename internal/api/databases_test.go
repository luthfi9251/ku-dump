package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

var errConnFail = errors.New("dial tcp: refused")

func newServerWithEngine(t *testing.T, pg engine.Engine) http.Handler {
	t.Helper()
	dir := t.TempDir()
	st, err := meta.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cx, err := cryptx.New(mustBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	newStore := func(ctx context.Context, destID int64) (storage.Store, error) {
		return storage.NewLocalFS(filepath.Join(dir, "dumps"))
	}
	engines := map[string]engine.Engine{"postgres": pg, "mongodb": fakeMongo{}}
	run, err := runner.New(st, engines, newStore, filepath.Join(dir, "dumps", "_logs"))
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(Deps{
		Store: st, Crypt: cx, Runner: run, Engines: engines, NewStore: newStore,
		Sessions: NewSessions([]byte("s")), Limiter: NewRateLimiter(),
	})
}

func validPayload() map[string]any {
	return map[string]any{
		"name": "prod", "engine": "postgres", "host": "db1", "port": 5432,
		"dbName": "app", "username": "u", "password": "pw", "options": `{"sslmode":"require"}`,
	}
}

func TestCreateDatabaseHappyPath(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "POST", "/api/databases", validPayload(), cookie)
	if rec.Code != 201 {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, "GET", "/api/databases", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("list = %d", rec.Code)
	}
	var dbs []databaseDTO
	json.NewDecoder(rec.Body).Decode(&dbs)
	if len(dbs) != 1 {
		t.Fatalf("len = %d", len(dbs))
	}
	d := dbs[0]
	if d.Name != "prod" || d.Engine != "postgres" || d.Port != 5432 || d.DBName != "app" ||
		d.Username != "u" || d.ToolsMissing != nil && len(d.ToolsMissing) > 0 {
		t.Fatalf("dto = %+v", d)
	}
	if d.ID == "" {
		t.Fatal("id empty")
	}
	if d.HasActiveJob {
		t.Fatal("hasActiveJob should be false")
	}
}

func TestCreateDatabaseAuthRequired(t *testing.T) {
	h := newTestServer(t)
	rec := doJSON(t, h, "POST", "/api/databases", validPayload(), nil)
	if rec.Code != 401 {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestCreateDatabaseValidation(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	cases := []map[string]any{
		{"engine": "postgres", "host": "h", "port": 5432, "dbName": "d", "username": "u", "password": "p"},
		{"name": "x", "engine": "mysql", "host": "h", "port": 5432, "dbName": "d", "username": "u", "password": "p"},
		{"name": "x", "engine": "postgres", "host": "", "port": 5432, "dbName": "d", "username": "u", "password": "p"},
		{"name": "x", "engine": "postgres", "host": "h", "port": 0, "dbName": "d", "username": "u", "password": "p"},
		{"name": "x", "engine": "postgres", "host": "h", "port": 5432, "dbName": "", "username": "u", "password": "p"},
		{"name": "x", "engine": "postgres", "host": "h", "port": 5432, "dbName": "d", "username": "u", "password": ""},
	}
	for i, body := range cases {
		rec := doJSON(t, h, "POST", "/api/databases", body, cookie)
		if rec.Code != 400 {
			t.Fatalf("case %d = %d: %s", i, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateDatabaseDuplicateName(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	doJSON(t, h, "POST", "/api/databases", validPayload(), cookie)
	rec := doJSON(t, h, "POST", "/api/databases", validPayload(), cookie)
	if rec.Code != 409 {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestCreateDatabaseBadOptions(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	p := validPayload()
	p["options"] = "{not-json"
	rec := doJSON(t, h, "POST", "/api/databases", p, cookie)
	if rec.Code != 400 {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestUpdateDatabaseKeepsPasswordWhenEmpty(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "POST", "/api/databases", validPayload(), cookie)
	var created databaseDTO
	json.NewDecoder(rec.Body).Decode(&created)
	p := validPayload()
	p["password"] = ""
	p["host"] = "db2"
	rec = doJSON(t, h, "PUT", "/api/databases/"+created.ID, p, cookie)
	if rec.Code != 200 {
		t.Fatalf("update = %d: %s", rec.Code, rec.Body.String())
	}
	var updated databaseDTO
	json.NewDecoder(rec.Body).Decode(&updated)
	if updated.Host != "db2" {
		t.Fatalf("host = %s", updated.Host)
	}
}

func TestUpdateDatabaseNotFound(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "PUT", "/api/databases/AAAA", validPayload(), cookie)
	if rec.Code != 404 {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestDeleteDatabase(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "POST", "/api/databases", validPayload(), cookie)
	var created databaseDTO
	json.NewDecoder(rec.Body).Decode(&created)
	rec = doJSON(t, h, "DELETE", "/api/databases/"+created.ID, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete = %d", rec.Code)
	}
	rec = doJSON(t, h, "GET", "/api/databases", nil, cookie)
	var dbs []databaseDTO
	json.NewDecoder(rec.Body).Decode(&dbs)
	if len(dbs) != 0 {
		t.Fatalf("len = %d", len(dbs))
	}
}

func TestTestDatabaseSetsResult(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "POST", "/api/databases", validPayload(), cookie)
	var created databaseDTO
	json.NewDecoder(rec.Body).Decode(&created)
	rec = doJSON(t, h, "POST", "/api/databases/"+created.ID+"/test", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("test = %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&res)
	if !res.OK {
		t.Fatalf("res = %+v", res)
	}
	rec = doJSON(t, h, "GET", "/api/databases", nil, cookie)
	var dbs []databaseDTO
	json.NewDecoder(rec.Body).Decode(&dbs)
	if dbs[0].LastTestOK == nil || !*dbs[0].LastTestOK || dbs[0].LastTestAt == nil {
		t.Fatalf("dto = %+v", dbs[0])
	}
}

func TestTestUnsavedPayload(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "POST", "/api/databases/test", validPayload(), cookie)
	if rec.Code != 200 {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		OK bool `json:"ok"`
	}
	json.NewDecoder(rec.Body).Decode(&res)
	if !res.OK {
		t.Fatal("res.ok false")
	}
	rec = doJSON(t, h, "GET", "/api/databases", nil, cookie)
	var dbs []databaseDTO
	json.NewDecoder(rec.Body).Decode(&dbs)
	if len(dbs) != 0 {
		t.Fatal("payload test should not persist")
	}
}

func TestCreateDatabaseConnectionFailure(t *testing.T) {
	h := newServerWithEngine(t, fakeEngine{connErr: errConnFail})
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "POST", "/api/databases", validPayload(), cookie)
	if rec.Code != 400 {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body.String())
	}
	var e struct {
		Error string `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&e)
	if e.Error != "CONNECTION_FAILED" {
		t.Fatalf("error = %s", e.Error)
	}
}
