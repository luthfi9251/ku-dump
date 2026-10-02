package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type destinationPayload struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	RootPath  string `json:"rootPath"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	AuthType  string `json:"authType"`
	RemoteDir string `json:"remoteDir"`
}

// normalize defaults an absent kind to s3 so pre-multi-kind clients (and old
// tests) keep working, and trims the free-form text fields.
func (p *destinationPayload) normalize() {
	if p.Kind == "" {
		p.Kind = "s3"
	}
	p.Name = strings.TrimSpace(p.Name)
	p.RootPath = strings.TrimSpace(p.RootPath)
}

func (p destinationPayload) validate(requireSecret bool) error {
	switch p.Kind {
	case "s3":
		if p.Name == "" || p.Endpoint == "" || p.Bucket == "" || p.AccessKey == "" {
			return errors.New("name, endpoint, bucket and accessKey are required")
		}
	case "local":
		if p.Name == "" || p.RootPath == "" {
			return errors.New("name and rootPath are required")
		}
		if !filepath.IsAbs(p.RootPath) {
			return errors.New("rootPath must be an absolute path")
		}
	case "sftp":
		if p.Name == "" || p.Host == "" || p.Username == "" {
			return errors.New("name, host and username are required")
		}
		if p.AuthType != "password" && p.AuthType != "key" {
			return errors.New("authType must be password or key")
		}
	default:
		return errors.New("kind must be s3, local or sftp")
	}
	if requireSecret && p.Kind != "local" && p.SecretKey == "" {
		return errors.New("secretKey is required")
	}
	return nil
}

type destinationDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretSet bool   `json:"secretSet"`
	RootPath  string `json:"rootPath"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	AuthType  string `json:"authType"`
	RemoteDir string `json:"remoteDir"`
	CreatedAt string `json:"createdAt"`
}

func destDTO(d *meta.Destination) destinationDTO {
	return destinationDTO{
		ID: encID(d.ID), Name: d.Name, Kind: d.Kind, Endpoint: d.Endpoint,
		Region: d.Region, Bucket: d.Bucket, Prefix: d.Prefix, AccessKey: d.AccessKey,
		SecretSet: d.SecretEnc != "", RootPath: d.RootPath, Host: d.Host, Port: d.Port,
		Username: d.Username, AuthType: d.AuthType, RemoteDir: d.RemoteDir,
		CreatedAt: d.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleListDestinations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListDestinations(r.Context())
	if err != nil {
		log.Printf("list destinations: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	out := make([]destinationDTO, 0, len(rows))
	for i := range rows {
		out = append(out, destDTO(&rows[i]))
	}
	jsonOut(w, http.StatusOK, out)
}

func (s *Server) destNameFree(ctx context.Context, name string, excludeID int64) bool {
	existing, err := s.Store.GetDestinationByName(ctx, name)
	if err != nil {
		return true
	}
	return existing.ID == excludeID
}

func (s *Server) handleCreateDestination(w http.ResponseWriter, r *http.Request) {
	var p destinationPayload
	if !decodeBody(w, r, &p) {
		return
	}
	p.normalize()
	if err := p.validate(true); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if !s.destNameFree(r.Context(), p.Name, 0) {
		fail(w, http.StatusConflict, "DUPLICATE_NAME", "destination name already used")
		return
	}
	secretEnc := ""
	if p.Kind != "local" {
		enc, err := s.Crypt.Encrypt(p.SecretKey)
		if err != nil {
			log.Printf("encrypt secret: %v", err)
			fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
		secretEnc = enc
	}
	if p.Kind == "local" {
		if err := s.probeLocalRoot(p.RootPath); err != nil {
			fail(w, http.StatusBadRequest, "VALIDATION",
				"rootPath is not usable: "+err.Error())
			return
		}
	}
	id, err := s.Store.CreateDestination(r.Context(), &meta.Destination{
		Name: p.Name, Kind: p.Kind, Endpoint: p.Endpoint, Region: p.Region,
		Bucket: p.Bucket, Prefix: p.Prefix, AccessKey: p.AccessKey, SecretEnc: secretEnc,
		RootPath: p.RootPath, Host: p.Host, Port: p.Port,
		Username: p.Username, AuthType: p.AuthType, RemoteDir: p.RemoteDir,
	})
	if err != nil {
		log.Printf("create destination: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	d, _ := s.Store.GetDestination(r.Context(), id)
	jsonOut(w, http.StatusCreated, destDTO(d))
}

func (s *Server) probeLocalRoot(rootPath string) error {
	l, err := storage.NewLocalFS(rootPath)
	if err != nil {
		return err
	}
	return l.Test()
}

func (s *Server) handleUpdateDestination(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	current, err := s.Store.GetDestination(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	var p destinationPayload
	if !decodeBody(w, r, &p) {
		return
	}
	p.normalize()
	if err := p.validate(false); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if p.Kind != current.Kind {
		fail(w, http.StatusBadRequest, "KIND_IMMUTABLE",
			"destination kind cannot change; delete and create a new one")
		return
	}
	if !s.destNameFree(r.Context(), p.Name, id) {
		fail(w, http.StatusConflict, "DUPLICATE_NAME", "destination name already used")
		return
	}
	secretEnc := current.SecretEnc
	if p.SecretKey != "" && current.Kind != "local" {
		enc, err := s.Crypt.Encrypt(p.SecretKey)
		if err != nil {
			log.Printf("encrypt secret: %v", err)
			fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
		secretEnc = enc
	}
	if current.Kind == "local" && p.RootPath != current.RootPath {
		if err := s.probeLocalRoot(p.RootPath); err != nil {
			fail(w, http.StatusBadRequest, "VALIDATION", "rootPath is not usable: "+err.Error())
			return
		}
	}
	updated := &meta.Destination{
		ID: id, Name: p.Name, Kind: current.Kind, Endpoint: p.Endpoint, Region: p.Region,
		Bucket: p.Bucket, Prefix: p.Prefix, AccessKey: p.AccessKey, SecretEnc: secretEnc,
		RootPath: p.RootPath, Host: p.Host, Port: p.Port,
		Username: p.Username, AuthType: p.AuthType, RemoteDir: p.RemoteDir,
	}
	if err := s.Store.UpdateDestination(r.Context(), updated); err != nil {
		log.Printf("update destination %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	d, _ := s.Store.GetDestination(r.Context(), id)
	jsonOut(w, http.StatusOK, destDTO(d))
}

func (s *Server) handleDeleteDestination(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	if _, err := s.Store.GetDestination(r.Context(), id); err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	n, err := s.Store.CountDumpsForDestination(r.Context(), id)
	if err != nil {
		log.Printf("count dumps for destination %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if n > 0 {
		fail(w, http.StatusConflict, "DESTINATION_IN_USE",
			"destination still holds dumps; delete or move them first")
		return
	}
	wn, err := s.Store.CountWorkflowsForDestination(r.Context(), id)
	if err != nil {
		log.Printf("count workflows for destination %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if wn > 0 {
		fail(w, http.StatusConflict, "DESTINATION_IN_USE",
			"destination is referenced by a workflow; update or delete the workflow first")
		return
	}
	if err := s.Store.DeleteDestination(r.Context(), id); err != nil {
		log.Printf("delete destination %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}

func testResponse(w http.ResponseWriter, err error) {
	if err != nil {
		jsonOut(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) testDestination(ctx context.Context, d *meta.Destination, secret string) error {
	switch d.Kind {
	case "local":
		l, err := storage.NewLocalFS(d.RootPath)
		if err != nil {
			return err
		}
		return l.Test()
	case "sftp":
		st, err := storage.NewSFTP(storage.SFTPConfig{
			Host: d.Host, Port: d.Port, Username: d.Username,
			AuthType: d.AuthType, Secret: secret, RemoteDir: d.RemoteDir,
		})
		if err != nil {
			return err
		}
		return st.Test(ctx)
	default:
		s3, err := storage.NewS3(storage.S3Config{
			Endpoint: d.Endpoint, Region: d.Region, Bucket: d.Bucket,
			Prefix: d.Prefix, AccessKey: d.AccessKey, SecretKey: secret,
		})
		if err != nil {
			return err
		}
		return s3.Test(ctx)
	}
}

func (s *Server) destSecret(ctx context.Context, d *meta.Destination) (string, error) {
	if d.SecretEnc == "" {
		return "", nil
	}
	return s.Crypt.Decrypt(d.SecretEnc)
}

func (s *Server) handleTestDestinationPayload(w http.ResponseWriter, r *http.Request) {
	var p destinationPayload
	if !decodeBody(w, r, &p) {
		return
	}
	p.normalize()
	if err := p.validate(true); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	d := &meta.Destination{
		Kind: p.Kind, Endpoint: p.Endpoint, Region: p.Region, Bucket: p.Bucket,
		Prefix: p.Prefix, AccessKey: p.AccessKey, RootPath: p.RootPath,
		Host: p.Host, Port: p.Port, Username: p.Username, AuthType: p.AuthType,
		RemoteDir: p.RemoteDir,
	}
	testResponse(w, s.testDestination(r.Context(), d, p.SecretKey))
}

func (s *Server) handleTestDestination(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	d, err := s.Store.GetDestination(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	secret, err := s.destSecret(r.Context(), d)
	if err != nil {
		log.Printf("decrypt secret dest %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	testResponse(w, s.testDestination(r.Context(), d, secret))
}
