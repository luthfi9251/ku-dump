package api

import (
	"context"
	"net/http"

	"github.com/luthfi9251/ku-dump/internal/storage"
)

const (
	keyS3Endpoint    = "s3_endpoint"
	keyS3Region      = "s3_region"
	keyS3Bucket      = "s3_bucket"
	keyS3Prefix      = "s3_prefix"
	keyS3AccessKey   = "s3_access_key"
	keyS3SecretEnc   = "s3_secret_enc"
	keySessionSecret = "session_secret"
)

type storageSettingsDTO struct {
	Endpoint   string `json:"endpoint"`
	Region     string `json:"region"`
	Bucket     string `json:"bucket"`
	Prefix     string `json:"prefix"`
	AccessKey  string `json:"accessKey"`
	SecretSet  bool   `json:"secretSet"`
	Configured bool   `json:"configured"`
}

type storageSettingsPayload struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

func (s *Server) s3Config(ctx context.Context) (storage.S3Config, bool) {
	get := func(k string) string {
		v, ok, _ := s.Store.GetSetting(ctx, k)
		if !ok {
			return ""
		}
		return v
	}
	encSecret := get(keyS3SecretEnc)
	secret, _ := s.Crypt.Decrypt(encSecret)
	cfg := storage.S3Config{
		Endpoint:  get(keyS3Endpoint),
		Region:    get(keyS3Region),
		Bucket:    get(keyS3Bucket),
		Prefix:    get(keyS3Prefix),
		AccessKey: get(keyS3AccessKey),
		SecretKey: secret,
	}
	configured := cfg.Endpoint != "" && cfg.Bucket != "" && cfg.AccessKey != "" && cfg.SecretKey != ""
	return cfg, configured
}

func (s *Server) handleGetStorageSettings(w http.ResponseWriter, r *http.Request) {
	cfg, configured := s.s3Config(r.Context())
	jsonOut(w, http.StatusOK, storageSettingsDTO{
		Endpoint: cfg.Endpoint, Region: cfg.Region, Bucket: cfg.Bucket,
		Prefix: cfg.Prefix, AccessKey: cfg.AccessKey,
		SecretSet: cfg.SecretKey != "", Configured: configured,
	})
}

func (s *Server) handlePutStorageSettings(w http.ResponseWriter, r *http.Request) {
	var p storageSettingsPayload
	if !decodeBody(w, r, &p) {
		return
	}
	if p.Endpoint != "" || p.Bucket != "" || p.AccessKey != "" {
		if p.Endpoint == "" || p.Bucket == "" || p.AccessKey == "" {
			fail(w, http.StatusBadRequest, "VALIDATION",
				"endpoint, bucket and accessKey must all be set together")
			return
		}
	}
	ctx := r.Context()
	set := func(k, v string) {
		if v != "" {
			_ = s.Store.SetSetting(ctx, k, v)
		}
	}
	set(keyS3Endpoint, p.Endpoint)
	set(keyS3Region, p.Region)
	set(keyS3Bucket, p.Bucket)
	set(keyS3Prefix, p.Prefix)
	set(keyS3AccessKey, p.AccessKey)
	if p.SecretKey != "" {
		enc, err := s.Crypt.Encrypt(p.SecretKey)
		if err != nil {
			fail(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}
		_ = s.Store.SetSetting(ctx, keyS3SecretEnc, enc)
	}
	s.handleGetStorageSettings(w, r)
}

func (s *Server) handleTestStorage(w http.ResponseWriter, r *http.Request) {
	cfg, configured := s.s3Config(r.Context())
	if !configured {
		fail(w, http.StatusBadRequest, "STORAGE_NOT_CONFIGURED", "configure S3 settings first")
		return
	}
	s3, err := storage.NewS3(cfg)
	if err != nil {
		fail(w, http.StatusBadRequest, "STORAGE_NOT_CONFIGURED", err.Error())
		return
	}
	if err := s3.Test(r.Context()); err != nil {
		jsonOut(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	out := map[string][]string{}
	for name, eng := range s.Engines {
		missing := eng.ToolsMissing()
		if missing == nil {
			missing = []string{}
		}
		out[name] = missing
	}
	jsonOut(w, http.StatusOK, out)
}
