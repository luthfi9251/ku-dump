package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/luthfi9251/ku-dump/internal/api"
	"github.com/luthfi9251/ku-dump/internal/config"
	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
	"github.com/luthfi9251/ku-dump/internal/scheduler"
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
	newStore := func(ctx context.Context, destID int64) (storage.Store, error) {
		if destID == 0 {
			return storage.NewLocalFS(cfg.DumpsDir)
		}
		d, err := st.GetDestination(ctx, destID)
		if err != nil {
			return nil, fmt.Errorf("storage destination %d not found", destID)
		}
		secret, err := cx.Decrypt(d.SecretEnc)
		if err != nil && d.Kind != "local" {
			return nil, fmt.Errorf("decrypt destination secret: %w", err)
		}
		switch d.Kind {
		case "local":
			return storage.NewLocalFS(d.RootPath)
		case "sftp":
			return storage.NewSFTP(storage.SFTPConfig{
				Host: d.Host, Port: d.Port, Username: d.Username,
				AuthType: d.AuthType, Secret: secret, RemoteDir: d.RemoteDir,
			})
		default:
			return storage.NewS3(storage.S3Config{
				Endpoint: d.Endpoint, Region: d.Region, Bucket: d.Bucket,
				Prefix: d.Prefix, AccessKey: d.AccessKey, SecretKey: secret,
			})
		}
	}

	if migrated, err := st.MigrateLegacyS3(ctx); err != nil {
		log.Fatal(err)
	} else if migrated {
		log.Printf("migrated legacy S3 settings into storage destination")
	}

	run, err := runner.New(st, engines, newStore, filepath.Join(cfg.DumpsDir, "_logs"))
	if err != nil {
		log.Fatal(err)
	}
	if err := run.Recover(ctx); err != nil {
		log.Fatal(err)
	}

	sched := scheduler.New(st, run)
	if err := sched.Recover(ctx, time.Now()); err != nil {
		log.Fatal(err)
	}
	schedCtx, stopSched := context.WithCancel(ctx)
	defer stopSched()
	go sched.Run(schedCtx)

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
