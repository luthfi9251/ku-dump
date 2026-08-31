package api

import (
	"encoding/json"
	"testing"
)

func TestStorageSettingsRoundTrip(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "GET", "/api/settings/storage", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("get = %d", rec.Code)
	}
	var got storageSettingsDTO
	json.NewDecoder(rec.Body).Decode(&got)
	if got.Configured || got.SecretSet {
		t.Fatalf("initial = %+v", got)
	}
	body := map[string]string{
		"endpoint": "http://127.0.0.1:9000", "region": "us-east-1",
		"bucket": "kudump", "prefix": "backups", "accessKey": "minioadmin", "secretKey": "minioadmin",
	}
	rec = doJSON(t, h, "PUT", "/api/settings/storage", body, cookie)
	if rec.Code != 200 {
		t.Fatalf("put = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, "GET", "/api/settings/storage", nil, cookie)
	json.NewDecoder(rec.Body).Decode(&got)
	if !got.Configured || !got.SecretSet || got.Bucket != "kudump" || got.Prefix != "backups" {
		t.Fatalf("after put = %+v", got)
	}
	delete(body, "secretKey")
	rec = doJSON(t, h, "PUT", "/api/settings/storage", body, cookie)
	if rec.Code != 200 {
		t.Fatalf("put keep secret = %d", rec.Code)
	}
	rec = doJSON(t, h, "GET", "/api/settings/storage", nil, cookie)
	json.NewDecoder(rec.Body).Decode(&got)
	if !got.SecretSet {
		t.Fatal("secret lost on empty PUT")
	}
}

func TestStorageSettingsValidation(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "PUT", "/api/settings/storage",
		map[string]string{"bucket": "only-bucket"}, cookie)
	if rec.Code != 400 {
		t.Fatalf("partial = %d", rec.Code)
	}
}

func TestStorageSettingsClear(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	body := map[string]string{
		"endpoint": "http://127.0.0.1:9000", "region": "us-east-1",
		"bucket": "kudump", "prefix": "backups", "accessKey": "minioadmin", "secretKey": "minioadmin",
	}
	rec := doJSON(t, h, "PUT", "/api/settings/storage", body, cookie)
	if rec.Code != 200 {
		t.Fatalf("put = %d: %s", rec.Code, rec.Body.String())
	}
	var got storageSettingsDTO
	rec = doJSON(t, h, "GET", "/api/settings/storage", nil, cookie)
	json.NewDecoder(rec.Body).Decode(&got)
	if !got.Configured || !got.SecretSet {
		t.Fatalf("after put = %+v", got)
	}
	rec = doJSON(t, h, "PUT", "/api/settings/storage",
		map[string]string{"endpoint": "", "region": "", "bucket": "", "prefix": "", "accessKey": ""},
		cookie)
	if rec.Code != 200 {
		t.Fatalf("put clear = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, "GET", "/api/settings/storage", nil, cookie)
	json.NewDecoder(rec.Body).Decode(&got)
	if got.Configured {
		t.Fatalf("configured should be false after clearing PUT: %+v", got)
	}
	if got.Bucket != "" || got.Endpoint != "" || got.Prefix != "" || got.AccessKey != "" {
		t.Fatalf("fields not cleared: %+v", got)
	}
}

func TestStorageSettingsTestNotConfigured(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "POST", "/api/settings/storage/test", nil, cookie)
	if rec.Code != 400 {
		t.Fatalf("code = %d", rec.Code)
	}
	var e struct {
		Error string `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&e)
	if e.Error != "STORAGE_NOT_CONFIGURED" {
		t.Fatalf("error = %s", e.Error)
	}
}

func TestStorageSettingsTestUnreachable(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "PUT", "/api/settings/storage", map[string]string{
		"endpoint": "http://127.0.0.1:1", "bucket": "kudump",
		"accessKey": "a", "secretKey": "s",
	}, cookie)
	if rec.Code != 200 {
		t.Fatalf("put = %d", rec.Code)
	}
	rec = doJSON(t, h, "POST", "/api/settings/storage/test", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("test = %d", rec.Code)
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&res)
	if res.OK || res.Error == "" {
		t.Fatalf("res = %+v", res)
	}
}

func TestToolsEndpoint(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "GET", "/api/tools", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var tools map[string][]string
	json.NewDecoder(rec.Body).Decode(&tools)
	if _, ok := tools["postgres"]; !ok {
		t.Fatalf("tools = %+v", tools)
	}
	if _, ok := tools["mongodb"]; !ok {
		t.Fatalf("tools = %+v", tools)
	}
}
