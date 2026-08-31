package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/luthfi9251/ku-dump/internal/api"
	"github.com/luthfi9251/ku-dump/internal/config"
	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
	"github.com/luthfi9251/ku-dump/internal/storage"
	"github.com/luthfi9251/ku-dump/web"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	keyPath := filepath.Join(filepath.Dir(cfg.DBPath), ".ku-dump-key")
	key, err := cryptx.LoadOrGenerateKey(cfg.CryptKey, keyPath)
	if err != nil {
		log.Fatal(err)
	}
	cx, err := cryptx.New(key)
	if err != nil {
		log.Fatal(err)
	}
	st, err := meta.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	secretHex, ok, err := st.GetSetting(ctx, "session_secret")
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			log.Fatal(err)
		}
		secretHex = hex.EncodeToString(raw)
		if err := st.SetSetting(ctx, "session_secret", secretHex); err != nil {
			log.Fatal(err)
		}
	}
	secret, err := hex.DecodeString(secretHex)
	if err != nil {
		log.Fatal(err)
	}

	engines := engine.BuildEngines(cfg.Tools, cx)
	newStore := func(kind string) (storage.Store, error) {
		switch kind {
		case "local":
			return storage.NewLocalFS(cfg.DumpsDir)
		case "s3":
			srv := &api.Server{Deps: api.Deps{Store: st, Crypt: cx}}
			s3cfg, configured := srv.S3ConfigPublic(ctx)
			if !configured {
				return nil, fmt.Errorf("s3 storage not configured")
			}
			return storage.NewS3(s3cfg)
		}
		return nil, fmt.Errorf("unknown storage %q", kind)
	}

	run, err := runner.New(st, engines, newStore, filepath.Join(cfg.DumpsDir, "_logs"))
	if err != nil {
		log.Fatal(err)
	}
	if err := run.Recover(ctx); err != nil {
		log.Fatal(err)
	}

	handler := api.NewServer(api.Deps{
		Store: st, Crypt: cx, Runner: run, Engines: engines, NewStore: newStore,
		Sessions: api.NewSessions(secret), Limiter: api.NewRateLimiter(),
	})
	mux := http.NewServeMux()
	mux.Handle("/api/", handler)
	mux.Handle("/", web.Handler())

	log.Printf("ku-dump listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}
