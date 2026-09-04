//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/luthfi9251/ku-dump/internal/api"
	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

const (
	pgHost   = "127.0.0.1"
	pgPort   = 55432
	mongoURI = "mongodb://127.0.0.1:28017"
)

func composeUp() bool {
	for attempt := 1; attempt <= 2; attempt++ {
		cmd := exec.Command("docker", "compose", "-f", "../docker-compose.test.yml", "up", "-d", "--wait")
		out, err := cmd.CombinedOutput()
		if err == nil {
			return true
		}
		fmt.Fprintf(os.Stderr, "compose up attempt %d failed: %v\n%s", attempt, err, out)
	}
	return false
}

func TestMain(m *testing.M) {
	if !composeUp() {
		fmt.Fprintln(os.Stderr, "compose up failed after retry")
		os.Exit(1)
	}
	code := m.Run()
	down := exec.Command("docker", "compose", "-f", "../docker-compose.test.yml", "down", "-v", "--remove-orphans")
	down.Run()
	os.Exit(code)
}

func tools(cx *cryptx.Cryptx) map[string]engine.Engine {
	return engine.BuildEngines(map[string]string{
		"pg_dump":      "testdata/bin/pg_dump.sh",
		"pg_restore":   "testdata/bin/pg_restore.sh",
		"mongodump":    "testdata/bin/mongodump.sh",
		"mongorestore": "testdata/bin/mongorestore.sh",
	}, cx)
}

func testCrypt(t *testing.T) *cryptx.Cryptx {
	t.Helper()
	c, err := cryptx.New(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func pgDatabase(t *testing.T, cx *cryptx.Cryptx, dbName string) meta.Database {
	t.Helper()
	enc, err := cx.Encrypt("postgres")
	if err != nil {
		t.Fatal(err)
	}
	return meta.Database{ID: 1, Name: "pg-" + dbName, Engine: "postgres", Host: pgHost, Port: pgPort,
		DBName: dbName, Username: "postgres", PasswordEnc: enc}
}

func waitReady(t *testing.T, what string, fn func() error) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := fn(); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("%s not ready: %v", what, lastErr)
}

func adminConn(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://postgres:postgres@%s:%d/postgres?sslmode=disable", pgHost, pgPort))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	return conn
}

func TestPostgresDumpRestore(t *testing.T) {
	cx := testCrypt(t)
	engines := tools(cx)
	ctx := context.Background()

	admin := adminConn(t)
	for _, db := range []string{"ku_src", "ku_dst"} {
		var dropped string
		_ = admin.QueryRow(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", db)).Scan(&dropped)
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+db); err != nil {
			t.Fatal(err)
		}
	}
	src, err := pgx.Connect(ctx, fmt.Sprintf("postgres://postgres:postgres@%s:%d/ku_src?sslmode=disable", pgHost, pgPort))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close(ctx)
	if _, err := src.Exec(ctx, `CREATE TABLE items (id serial PRIMARY KEY, name text);
		INSERT INTO items (name) VALUES ('alpha'), ('beta'), ('gamma')`); err != nil {
		t.Fatal(err)
	}

	pg := engines["postgres"]
	if err := pg.TestConnection(ctx, pgDatabase(t, cx, "ku_src")); err != nil {
		t.Fatalf("test connection: %v", err)
	}
	var dump bytes.Buffer
	var logBuf bytes.Buffer
	if err := pg.Dump(ctx, pgDatabase(t, cx, "ku_src"), &dump, &logBuf); err != nil {
		t.Fatalf("dump: %v\nlog: %s", err, logBuf.String())
	}
	if !bytes.Contains(dump.Bytes(), []byte("PGDMP")) {
		t.Fatalf("dump does not look like -Fc archive (first bytes: %q)", dump.Bytes()[:5])
	}

	dst := pgDatabase(t, cx, "ku_dst")
	if err := pg.Restore(ctx, dst, "ku_src", bytes.NewReader(dump.Bytes()), &logBuf); err != nil {
		t.Fatalf("restore: %v\nlog: %s", err, logBuf.String())
	}
	dstConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://postgres:postgres@%s:%d/ku_dst?sslmode=disable", pgHost, pgPort))
	if err != nil {
		t.Fatal(err)
	}
	defer dstConn.Close(ctx)
	var count int
	if err := dstConn.QueryRow(ctx, `SELECT COUNT(*) FROM items`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("restored count = %d, want 3", count)
	}
}

func mongoClient(t *testing.T) *mongo.Client {
	t.Helper()
	ctx := context.Background()
	cli, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cli.Disconnect(ctx) })
	waitReady(t, "mongo", func() error {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		return cli.Ping(cctx, nil)
	})
	return cli
}

func TestMongoDumpRestoreRemap(t *testing.T) {
	cx := testCrypt(t)
	engines := tools(cx)
	ctx := context.Background()
	cli := mongoClient(t)
	if err := cli.Database("ku_src").Drop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cli.Database("ku_dst").Drop(ctx); err != nil {
		t.Fatal(err)
	}
	docs := []any{bson.M{"n": 1}, bson.M{"n": 2}, bson.M{"n": 3}, bson.M{"n": 4}, bson.M{"n": 5}}
	if _, err := cli.Database("ku_src").Collection("c").InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}

	enc, _ := cx.Encrypt("")
	src := meta.Database{ID: 1, Name: "mongo-src", Engine: "mongodb", Host: "127.0.0.1", Port: 28017,
		DBName: "ku_src", Username: "", PasswordEnc: enc}
	mgo := engines["mongodb"]
	var dump bytes.Buffer
	var logBuf bytes.Buffer
	if err := mgo.Dump(ctx, src, &dump, &logBuf); err != nil {
		t.Fatalf("dump: %v\nlog: %s", err, logBuf.String())
	}

	dst := src
	dst.DBName = "ku_dst"
	if err := mgo.Restore(ctx, dst, "ku_src", bytes.NewReader(dump.Bytes()), &logBuf); err != nil {
		t.Fatalf("restore: %v\nlog: %s", err, logBuf.String())
	}
	count, err := cli.Database("ku_dst").Collection("c").CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("restored count = %d, want 5", count)
	}
}

func TestS3RoundTrip(t *testing.T) {
	s3, err := storage.NewS3(storage.S3Config{
		Endpoint: "http://127.0.0.1:9000", Region: "us-east-1",
		Bucket: "ku-dump-test", AccessKey: "minioadmin", SecretKey: "minioadmin",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s3.Test(ctx); err == nil {
		return
	}
	cli, err := s3.RawClient()
	if err != nil {
		t.Skipf("minio not available: %v", err)
	}
	if err := cli.MakeBucket(ctx, "ku-dump-test", minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "x.dump")
	os.WriteFile(src, []byte("S3-ROUNDTRIP"), 0o644)
	if err := s3.Put(ctx, "test/x.dump", src); err != nil {
		t.Fatal(err)
	}
	rc, err := s3.Open(ctx, "test/x.dump")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "S3-ROUNDTRIP" {
		t.Fatalf("body = %q", b)
	}
	if err := s3.Delete(ctx, "test/x.dump"); err != nil {
		t.Fatal(err)
	}
	rc, err = s3.Open(ctx, "test/x.dump")
	if err == nil {
		_, err = io.ReadAll(rc)
		rc.Close()
	}
	if err == nil {
		t.Fatal("deleted object still readable")
	}
}

func postJSON(t *testing.T, client *http.Client, base, path string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest("POST", base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func getJSON(t *testing.T, client *http.Client, base, path string) (int, []byte) {
	t.Helper()
	res, err := client.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func delJSON(t *testing.T, client *http.Client, base, path string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest("DELETE", base+path, nil)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func waitJobSuccess(t *testing.T, client *http.Client, base, jobID string) {
	t.Helper()
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		code, body := getJSON(t, client, base, "/api/jobs/"+jobID)
		if code == 200 {
			var j struct {
				Status string `json:"status"`
				Error  string `json:"error"`
			}
			json.Unmarshal(body, &j)
			if j.Status == "success" {
				return
			}
			if j.Status == "failed" || j.Status == "cancelled" {
				t.Fatalf("job %s: %s", j.Status, j.Error)
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("job did not finish in time")
}

func TestAPIEndToEnd(t *testing.T) {
	cx := testCrypt(t)
	engines := tools(cx)
	dir := t.TempDir()
	st, err := meta.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dumpsDir := filepath.Join(dir, "dumps")
	newStore := func(ctx context.Context, destID int64) (storage.Store, error) {
		if destID == 0 {
			return storage.NewLocalFS(dumpsDir)
		}
		d, err := st.GetDestination(ctx, destID)
		if err != nil {
			return nil, fmt.Errorf("destination %d not found", destID)
		}
		secret, err := cx.Decrypt(d.SecretEnc)
		if err != nil {
			return nil, err
		}
		return storage.NewS3(storage.S3Config{
			Endpoint: d.Endpoint, Region: d.Region, Bucket: d.Bucket,
			Prefix: d.Prefix, AccessKey: d.AccessKey, SecretKey: secret,
		})
	}
	run, err := runner.New(st, engines, newStore, filepath.Join(dumpsDir, "_logs"))
	if err != nil {
		t.Fatal(err)
	}
	handler := api.NewServer(api.Deps{
		Store: st, Crypt: cx, Runner: run, Engines: engines, NewStore: newStore,
		Sessions: api.NewSessions([]byte("it-secret")), Limiter: api.NewRateLimiter(),
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}

	if code, _ := getJSON(t, client, srv.URL, "/api/auth/status"); code != 200 {
		t.Fatalf("status = %d", code)
	}
	if code, _ := postJSON(t, client, srv.URL, "/api/auth/setup", map[string]string{"username": "admin", "password": "password123"}); code != 201 {
		t.Fatal("setup failed")
	}
	if code, _ := postJSON(t, client, srv.URL, "/api/auth/login", map[string]string{"username": "admin", "password": "password123"}); code != 200 {
		t.Fatal("login failed")
	}

	admin := adminConn(t)
	for _, db := range []string{"ku_api_src", "ku_api_dst"} {
		var dropped string
		_ = admin.QueryRow(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", db)).Scan(&dropped)
		if _, err := admin.Exec(context.Background(), "CREATE DATABASE "+db); err != nil {
			t.Fatal(err)
		}
	}
	srcConn, err := pgx.Connect(context.Background(), fmt.Sprintf("postgres://postgres:postgres@%s:%d/ku_api_src?sslmode=disable", pgHost, pgPort))
	if err != nil {
		t.Fatal(err)
	}
	defer srcConn.Close(context.Background())
	if _, err := srcConn.Exec(context.Background(), `CREATE TABLE t (id serial PRIMARY KEY, v text);
		INSERT INTO t (v) VALUES ('one'), ('two')`); err != nil {
		t.Fatal(err)
	}

	code, body := postJSON(t, client, srv.URL, "/api/databases", map[string]any{
		"name": "api-src", "engine": "postgres", "host": pgHost, "port": pgPort,
		"dbName": "ku_api_src", "username": "postgres", "password": "postgres",
	})
	if code != 201 {
		t.Fatalf("register src = %d: %s", code, body)
	}
	code, body = postJSON(t, client, srv.URL, "/api/databases", map[string]any{
		"name": "api-dst", "engine": "postgres", "host": pgHost, "port": pgPort,
		"dbName": "ku_api_dst", "username": "postgres", "password": "postgres",
	})
	if code != 201 {
		t.Fatalf("register dst = %d: %s", code, body)
	}

	code, body = postJSON(t, client, srv.URL, "/api/databases/MTIz/dump", map[string]string{"label": "e2e", "destId": ""})
	if code == 201 {
		t.Fatal("opaque id guessing must not pass")
	}
	var dbs []map[string]any
	code, body = getJSON(t, client, srv.URL, "/api/databases")
	json.Unmarshal(body, &dbs)
	var srcID, dstID string
	for _, db := range dbs {
		if db["name"] == "api-src" {
			srcID = db["id"].(string)
		}
		if db["name"] == "api-dst" {
			dstID = db["id"].(string)
		}
	}
	code, body = postJSON(t, client, srv.URL, fmt.Sprintf("/api/databases/%s/dump", srcID), map[string]string{"label": "e2e", "destId": ""})
	if code != 201 {
		t.Fatalf("dump = %d: %s", code, body)
	}
	var dumpRes struct {
		JobID  string `json:"jobId"`
		DumpID string `json:"dumpId"`
	}
	json.Unmarshal(body, &dumpRes)
	waitJobSuccess(t, client, srv.URL, dumpRes.JobID)

	code, body = getJSON(t, client, srv.URL, "/api/dumps")
	var dumps []map[string]any
	json.Unmarshal(body, &dumps)
	var dumpID string
	for _, d := range dumps {
		if d["label"] == "e2e" {
			dumpID = d["id"].(string)
		}
	}
	if dumpID == "" {
		t.Fatal("dump not listed")
	}

	code, body = postJSON(t, client, srv.URL, "/api/restores", map[string]string{
		"dumpId": dumpID, "targetDatabaseId": dstID, "confirmName": "api-dst",
	})
	if code != 201 {
		t.Fatalf("restore = %d: %s", code, body)
	}
	var restoreRes struct {
		JobID string `json:"jobId"`
	}
	json.Unmarshal(body, &restoreRes)
	waitJobSuccess(t, client, srv.URL, restoreRes.JobID)

	dstConn, err := pgx.Connect(context.Background(), fmt.Sprintf("postgres://postgres:postgres@%s:%d/ku_api_dst?sslmode=disable", pgHost, pgPort))
	if err != nil {
		t.Fatal(err)
	}
	defer dstConn.Close(context.Background())
	var count int
	if err := dstConn.QueryRow(context.Background(), `SELECT COUNT(*) FROM t`).Scan(&count); err != nil {
		t.Fatalf("query restored table: %v (was restore actually applied?)", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

func TestAPIS3DestinationLifecycle(t *testing.T) {
	cx := testCrypt(t)
	engines := tools(cx)
	dir := t.TempDir()
	st, err := meta.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.MigrateLegacyS3(context.Background()); err != nil {
		t.Fatal(err)
	}
	dumpsDir := filepath.Join(dir, "dumps")
	newStore := func(ctx context.Context, destID int64) (storage.Store, error) {
		if destID == 0 {
			return storage.NewLocalFS(dumpsDir)
		}
		d, err := st.GetDestination(ctx, destID)
		if err != nil {
			return nil, fmt.Errorf("destination %d not found", destID)
		}
		secret, err := cx.Decrypt(d.SecretEnc)
		if err != nil {
			return nil, err
		}
		return storage.NewS3(storage.S3Config{
			Endpoint: d.Endpoint, Region: d.Region, Bucket: d.Bucket,
			Prefix: d.Prefix, AccessKey: d.AccessKey, SecretKey: secret,
		})
	}
	run, err := runner.New(st, engines, newStore, filepath.Join(dumpsDir, "_logs"))
	if err != nil {
		t.Fatal(err)
	}
	handler := api.NewServer(api.Deps{
		Store: st, Crypt: cx, Runner: run, Engines: engines, NewStore: newStore,
		Sessions: api.NewSessions([]byte("it-secret")), Limiter: api.NewRateLimiter(),
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}

	cli, err := minio.New("127.0.0.1:9000", &minio.Options{
		Creds:  credentials.NewStaticV4("minioadmin", "minioadmin", ""),
		Secure: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	bucket := "ku-dump-api"
	ok, err := cli.BucketExists(ctx, bucket)
	if err != nil {
		t.Skipf("minio not available: %v", err)
	}
	if !ok {
		if err := cli.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	if code, _ := postJSON(t, client, srv.URL, "/api/auth/setup", map[string]string{"username": "admin", "password": "password123"}); code != 201 {
		t.Fatal("setup failed")
	}
	if code, _ := postJSON(t, client, srv.URL, "/api/auth/login", map[string]string{"username": "admin", "password": "password123"}); code != 200 {
		t.Fatal("login failed")
	}

	admin := adminConn(t)
	for _, db := range []string{"ku_api_s3src", "ku_api_s3dst"} {
		var dropped string
		_ = admin.QueryRow(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", db)).Scan(&dropped)
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+db); err != nil {
			t.Fatal(err)
		}
	}
	srcConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://postgres:postgres@%s:%d/ku_api_s3src?sslmode=disable", pgHost, pgPort))
	if err != nil {
		t.Fatal(err)
	}
	defer srcConn.Close(ctx)
	if _, err := srcConn.Exec(ctx, `CREATE TABLE t (id serial PRIMARY KEY, v text);
		INSERT INTO t (v) VALUES ('one'), ('two')`); err != nil {
		t.Fatal(err)
	}

	code, body := postJSON(t, client, srv.URL, "/api/storage/destinations", map[string]any{
		"name": "it-minio", "endpoint": "http://127.0.0.1:9000", "region": "us-east-1",
		"bucket": bucket, "prefix": "it", "accessKey": "minioadmin", "secretKey": "minioadmin",
	})
	if code != 201 {
		t.Fatalf("create dest = %d: %s", code, body)
	}
	var dest struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &dest)
	if dest.ID == "" {
		t.Fatal("dest id empty")
	}

	for _, db := range []map[string]any{
		{"name": "api-s3-src", "engine": "postgres", "host": pgHost, "port": pgPort,
			"dbName": "ku_api_s3src", "username": "postgres", "password": "postgres"},
		{"name": "api-s3-dst", "engine": "postgres", "host": pgHost, "port": pgPort,
			"dbName": "ku_api_s3dst", "username": "postgres", "password": "postgres"},
	} {
		if c, b := postJSON(t, client, srv.URL, "/api/databases", db); c != 201 {
			t.Fatalf("register %v = %d: %s", db["name"], c, b)
		}
	}
	_, body = getJSON(t, client, srv.URL, "/api/databases")
	var dbs []map[string]any
	json.Unmarshal(body, &dbs)
	var srcID, dstID string
	for _, db := range dbs {
		if db["name"] == "api-s3-src" {
			srcID = db["id"].(string)
		}
		if db["name"] == "api-s3-dst" {
			dstID = db["id"].(string)
		}
	}

	code, body = postJSON(t, client, srv.URL, fmt.Sprintf("/api/databases/%s/dump", srcID), map[string]string{"label": "s3e2e", "destId": dest.ID})
	if code != 201 {
		t.Fatalf("dump = %d: %s", code, body)
	}
	var dumpRes struct {
		JobID  string `json:"jobId"`
		DumpID string `json:"dumpId"`
	}
	json.Unmarshal(body, &dumpRes)
	waitJobSuccess(t, client, srv.URL, dumpRes.JobID)

	code, body = getJSON(t, client, srv.URL, "/api/dumps")
	var dumps []map[string]any
	json.Unmarshal(body, &dumps)
	var dumpID string
	for _, d := range dumps {
		if d["label"] == "s3e2e" {
			dumpID = d["id"].(string)
			if d["destName"] != "it-minio" {
				t.Fatalf("destName = %v", d["destName"])
			}
		}
	}
	if dumpID == "" {
		t.Fatal("s3 dump not listed")
	}

	code, body = delJSON(t, client, srv.URL, "/api/storage/destinations/"+dest.ID)
	if code != 409 {
		t.Fatalf("delete in-use dest = %d: %s", code, body)
	}

	code, body = postJSON(t, client, srv.URL, "/api/restores", map[string]string{
		"dumpId": dumpID, "targetDatabaseId": dstID, "confirmName": "api-s3-dst",
	})
	if code != 201 {
		t.Fatalf("restore = %d: %s", code, body)
	}
	var restoreRes struct {
		JobID string `json:"jobId"`
	}
	json.Unmarshal(body, &restoreRes)
	waitJobSuccess(t, client, srv.URL, restoreRes.JobID)

	dstConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://postgres:postgres@%s:%d/ku_api_s3dst?sslmode=disable", pgHost, pgPort))
	if err != nil {
		t.Fatal(err)
	}
	defer dstConn.Close(ctx)
	var count int
	if err := dstConn.QueryRow(ctx, `SELECT COUNT(*) FROM t`).Scan(&count); err != nil {
		t.Fatalf("query restored table: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	if code, _ := delJSON(t, client, srv.URL, "/api/dumps/"+dumpID); code != 200 {
		t.Fatalf("delete dump = %d", code)
	}
	if code, _ := delJSON(t, client, srv.URL, "/api/storage/destinations/"+dest.ID); code != 200 {
		t.Fatalf("delete dest after dump gone = %d", code)
	}
}
