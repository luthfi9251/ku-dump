package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type fakeEngine struct{ connErr error }

func (f fakeEngine) Name() string { return "postgres" }
func (f fakeEngine) RequiredTools() []string {
	return nil
}
func (f fakeEngine) ToolsMissing() []string { return nil }
func (f fakeEngine) TestConnection(ctx context.Context, db meta.Database) error {
	return f.connErr
}
func (f fakeEngine) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	io.WriteString(log, "dumping\n")
	io.WriteString(out, "DUMPDATA:"+db.DBName)
	return nil
}
func (f fakeEngine) Restore(ctx context.Context, db meta.Database, sourceDB string, in io.Reader, log io.Writer) error {
	io.Copy(log, in)
	if db.DBName == "slow" {
		time.Sleep(2 * time.Second)
	}
	return nil
}

type fakeMongo struct{ fakeEngine }

func (fakeMongo) Name() string { return "mongodb" }

func newTestServer(t *testing.T) http.Handler {
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
	newStore := func(kind string) (storage.Store, error) {
		if kind == "local" {
			return storage.NewLocalFS(filepath.Join(dir, "dumps"))
		}
		if kind == "s3" {
			srv := &Server{Deps: Deps{Store: st, Crypt: cx}}
			cfg, configured := srv.s3Config(context.Background())
			if !configured {
				return nil, fmt.Errorf("s3 storage not configured")
			}
			return storage.NewS3(cfg)
		}
		return nil, fmt.Errorf("unknown storage %q", kind)
	}
	engines := map[string]engine.Engine{"postgres": fakeEngine{}, "mongodb": fakeMongo{}}
	run, err := runner.New(st, engines, newStore, filepath.Join(dir, "dumps", "_logs"))
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(Deps{
		Store: st, Crypt: cx, Runner: run, Engines: engines, NewStore: newStore,
		Sessions: NewSessions([]byte("test-session-secret")), Limiter: NewRateLimiter(),
	})
}

func mustBytes(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func setupAndLogin(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	rec := doJSON(t, h, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "password123"}, nil)
	if rec.Code != 201 {
		t.Fatalf("setup = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "password123"}, nil)
	if rec.Code != 200 {
		t.Fatalf("login = %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()[0]
}

func TestAuthStatusNeedsSetup(t *testing.T) {
	h := newTestServer(t)
	rec := doJSON(t, h, "GET", "/api/auth/status", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		NeedsSetup bool `json:"needsSetup"`
	}
	json.NewDecoder(rec.Body).Decode(&got)
	if !got.NeedsSetup {
		t.Fatal("needsSetup should be true")
	}
}

func TestSetupLoginMeLogout(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	if cookie.Name != "ku_dump_session" {
		t.Fatalf("cookie = %s", cookie.Name)
	}
	rec := doJSON(t, h, "GET", "/api/me", nil, nil)
	if rec.Code != 401 {
		t.Fatalf("me without cookie = %d", rec.Code)
	}
	rec = doJSON(t, h, "GET", "/api/me", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("me = %d: %s", rec.Code, rec.Body.String())
	}
	var me struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	json.NewDecoder(rec.Body).Decode(&me)
	if me.Username != "admin" || me.ID == "" {
		t.Fatalf("me = %+v", me)
	}
	rec = doJSON(t, h, "POST", "/api/auth/logout", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("logout = %d", rec.Code)
	}
	rec = doJSON(t, h, "POST", "/api/auth/setup", map[string]string{"username": "x", "password": "password123"}, nil)
	if rec.Code != 409 {
		t.Fatalf("second setup = %d", rec.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	h := newTestServer(t)
	setupAndLogin(t, h)
	for i := 0; i < 5; i++ {
		rec := doJSON(t, h, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong"}, nil)
		if rec.Code != 401 {
			t.Fatalf("attempt %d = %d", i, rec.Code)
		}
	}
	rec := doJSON(t, h, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "password123"}, nil)
	if rec.Code != 429 {
		t.Fatalf("6th attempt = %d", rec.Code)
	}
}

func TestSetupValidation(t *testing.T) {
	h := newTestServer(t)
	rec := doJSON(t, h, "POST", "/api/auth/setup", map[string]string{"username": "ab", "password": "password123"}, nil)
	if rec.Code != 400 {
		t.Fatalf("short username = %d", rec.Code)
	}
	rec = doJSON(t, h, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "short"}, nil)
	if rec.Code != 400 {
		t.Fatalf("short password = %d", rec.Code)
	}
}

func TestUnknownRoute(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "GET", "/api/nope", nil, cookie)
	if rec.Code != 404 {
		t.Fatalf("code = %d", rec.Code)
	}
	var e struct {
		Error string `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&e)
	if e.Error != "NOT_FOUND" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestSessionsToken(t *testing.T) {
	s := NewSessions([]byte("secret"))
	tok := s.NewToken(42)
	uid, ok := s.Parse(tok)
	if !ok || uid != 42 {
		t.Fatalf("parse = %d, %v", uid, ok)
	}
	if _, ok := s.Parse(tok + "x"); ok {
		t.Fatal("tampered token accepted")
	}
	if _, ok := s.Parse("garbage"); ok {
		t.Fatal("garbage accepted")
	}
	expired := NewSessions([]byte("secret"))
	expired.ttl = -time.Hour
	if _, ok := expired.Parse(expired.NewToken(1)); ok {
		t.Fatal("expired token accepted")
	}
}

func TestEncDecID(t *testing.T) {
	s := encID(12345)
	id, err := decID(s)
	if err != nil || id != 12345 {
		t.Fatalf("roundtrip = %d, %v", id, err)
	}
	if _, err := decID("not-base64!!"); err == nil {
		t.Fatal("bad id accepted")
	}
}
