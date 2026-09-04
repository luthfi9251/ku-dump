# Multi Storage Destination — Chunk 3: Integration E2E + README

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Suite integration docker (postgres + minio) membuktikan siklus penuh: buat destination via API → dump ke S3 → delete-guard → restore dari S3 ke database lain → bersih-bersih. README disesuaikan.

**Architecture:** Menambah satu test function baru dan menyesuaikan helper `newStore`/payload pada test e2e yang ada.

**Tech Stack:** Go test tags `integration`, docker compose (`../docker-compose.test.yml`), minio-go.

**Prerequisite:** Chunk 1 + Chunk 2 merged.

## Global Constraints

- Test integration jalan dengan: `make integration` (butuh docker).
- Jangan ubah perilaku test lain; hanya signature/payload yang berubah karena API baru.
- Bucket minio untuk test API: `ku-dump-api` (dibuat test, dihapus compose down -v).

---

### Task 1: Update test integration yang ada

**Files:**
- Modify: `integration/integration_test.go`

- [ ] **Step 1: Ganti `newStore` di `TestAPIEndToEnd`**

```go
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
```

- [ ] **Step 2: Ganti payload dump**

Dua panggilan `post(..."/dump", map[string]string{"label": "e2e", "storage": "local"})` → `{"label": "e2e", "destId": ""}`.

- [ ] **Step 3: Compile check**

Run: `go vet -tags integration ./integration/...`
Expected: lulus (belum dijalankan).

---

### Task 2: Test lifecycle destination S3 via API

**Files:**
- Modify: `integration/integration_test.go`

- [ ] **Step 1: Tambah test (failing tanpa chunk 1/2 — di sini harus langsung hijau)**

```go
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
	post := func(path string, body any) (int, []byte) { /* sama dengan helper di TestAPIEndToEnd */ }
	get := func(path string) (int, []byte) { /* sama */ }
	del := func(path string) (int, []byte) {
		req, _ := http.NewRequest("DELETE", srv.URL+path, nil)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, b
	}

	// setup auth + dua database pg (ku_api_s3src, ku_api_s3dst), seperti pola TestAPIEndToEnd
	// (DROP DATABASE IF EXISTS ... WITH (FORCE), CREATE DATABASE, tabel t + 2 rows di src)

	// siapkan bucket minio
	cli, err := minio.New("127.0.0.1:9000", &minio.Options{
		Creds: credentials.NewStaticV4("minioadmin", "minioadmin", ""), Secure: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	bucket := "ku-dump-api"
	exists, err := cli.BucketExists(ctx, bucket)
	if err != nil {
		t.Skipf("minio not available: %v", err)
	}
	if !exists {
		if err := cli.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	if code, _ := post("/api/auth/setup", map[string]string{"username": "admin", "password": "password123"}); code != 201 {
		t.Fatal("setup failed")
	}
	if code, _ := post("/api/auth/login", map[string]string{"username": "admin", "password": "password123"}); code != 200 {
		t.Fatal("login failed")
	}

	// 1. create destination
	code, body := post("/api/storage/destinations", map[string]any{
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

	// 2. register dua database (s3-src, s3-dst) via /api/databases — pola sama dengan TestAPIEndToEnd
	// 3. dump src dengan destId -> tunggu job success
	_, body = get("/api/databases")
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
	code, body = post(fmt.Sprintf("/api/databases/%s/dump", srcID), map[string]string{"label": "s3e2e", "destId": dest.ID})
	if code != 201 {
		t.Fatalf("dump = %d: %s", code, body)
	}
	var dumpRes struct {
		JobID  string `json:"jobId"`
		DumpID string `json:"dumpId"`
	}
	json.Unmarshal(body, &dumpRes)
	waitJobSuccess(t, client, srv.URL, dumpRes.JobID)

	// 4. dump tercatat dengan destName
	code, body = get("/api/dumps")
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

	// 5. delete-guard
	code, body = del("/api/storage/destinations/" + dest.ID)
	if code != 409 {
		t.Fatalf("delete in-use = %d: %s", code, body)
	}

	// 6. restore dari dump S3 ke database lain (bukan source) -> success + data sesuai
	code, body = post("/api/restores", map[string]string{
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
	// hitung rows di ku_api_s3dst == 2 (pola TestAPIEndToEnd)

	// 7. cleanup: delete dump lalu destination
	if code, _ := del("/api/dumps/" + dumpID); code != 200 {
		t.Fatalf("delete dump = %d", code)
	}
	if code, _ := del("/api/storage/destinations/" + dest.ID); code != 200 {
		t.Fatalf("delete dest = %d", code)
	}
}

func waitJobSuccess(t *testing.T, client *http.Client, base, jobID string) {
	t.Helper()
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		res, err := client.Get(base + "/api/jobs/" + jobID)
		if err == nil {
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			var j struct {
				Status string `json:"status"`
				Error  string `json:"error"`
			}
			json.Unmarshal(b, &j)
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
```

Catatan implementasi: `post`/`get` di `TestAPIEndToEnd` saat ini adalah closure lokal — jadikan helper package-level (`postJSON(t, client, base, path, body)` / `getJSON(...)`) agar dipakai dua test, ATAU duplikasi closure di test baru. Pilih refactor helper package-level (DRY), ubah `TestAPIEndToEnd` memakainya.

- [ ] **Step 2: Jalankan suite integration**

Run: `make integration`
Expected: semua PASS termasuk `TestAPIS3DestinationLifecycle`.

- [ ] **Step 3: Commit**

```bash
git add integration/integration_test.go
git commit -m "test(integration): s3 destination lifecycle via API"
```

---

### Task 3: README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Edit**

- Bagian **Usage** poin **Settings**: ganti deskripsi jadi "Storage destinations — kelola beberapa S3-compatible destination (endpoint/bucket/credentials, MinIO works) dan CLI tool availability."
- Bagian **Usage** poin **Databases/Dumps**: sebutkan dump memilih destination penyimpanan; restore bebas memilih dump (dari destination mana pun) dan target database engine-sama.
- Bagian **Security notes**: "Database passwords and S3 secrets are encrypted at rest (AES-256-GCM)" (plural).

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: multi storage destination usage"
```

---

## Verification Chunk 3 (final gate)

```bash
go build ./... && go vet ./... && go test ./...
make integration
npm --prefix web run build
make build
```

Semua hijau = fitur selesai: user bisa mendaftarkan banyak S3 destination, memilih database + tujuan penyimpanan saat dump, dan saat restore memilih dump dari destination mana pun untuk direstore ke database target mana pun (engine sama, konfirmasi nama wajib).
