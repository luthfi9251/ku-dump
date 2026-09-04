package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type destinationPayload struct {
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

func (p destinationPayload) validate(requireSecret bool) error {
	if strings.TrimSpace(p.Name) == "" || p.Endpoint == "" || p.Bucket == "" || p.AccessKey == "" {
		return errors.New("name, endpoint, bucket and accessKey are required")
	}
	if requireSecret && p.SecretKey == "" {
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
	CreatedAt string `json:"createdAt"`
}

func destDTO(d *meta.Destination) destinationDTO {
	return destinationDTO{
		ID: encID(d.ID), Name: d.Name, Kind: d.Kind, Endpoint: d.Endpoint,
		Region: d.Region, Bucket: d.Bucket, Prefix: d.Prefix, AccessKey: d.AccessKey,
		SecretSet: d.SecretEnc != "", CreatedAt: d.CreatedAt.Format(time.RFC3339),
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
	if err := p.validate(true); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if !s.destNameFree(r.Context(), p.Name, 0) {
		fail(w, http.StatusConflict, "DUPLICATE_NAME", "destination name already used")
		return
	}
	enc, err := s.Crypt.Encrypt(p.SecretKey)
	if err != nil {
		log.Printf("encrypt secret: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	id, err := s.Store.CreateDestination(r.Context(), &meta.Destination{
		Name: p.Name, Kind: "s3", Endpoint: p.Endpoint, Region: p.Region,
		Bucket: p.Bucket, Prefix: p.Prefix, AccessKey: p.AccessKey, SecretEnc: enc,
	})
	if err != nil {
		log.Printf("create destination: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	d, _ := s.Store.GetDestination(r.Context(), id)
	jsonOut(w, http.StatusCreated, destDTO(d))
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
	if err := p.validate(false); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if !s.destNameFree(r.Context(), p.Name, id) {
		fail(w, http.StatusConflict, "DUPLICATE_NAME", "destination name already used")
		return
	}
	secretEnc := current.SecretEnc
	if p.SecretKey != "" {
		enc, err := s.Crypt.Encrypt(p.SecretKey)
		if err != nil {
			log.Printf("encrypt secret: %v", err)
			fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
		secretEnc = enc
	}
	updated := &meta.Destination{
		ID: id, Name: p.Name, Kind: current.Kind, Endpoint: p.Endpoint, Region: p.Region,
		Bucket: p.Bucket, Prefix: p.Prefix, AccessKey: p.AccessKey, SecretEnc: secretEnc,
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
	if err := s.Store.DeleteDestination(r.Context(), id); err != nil {
		log.Printf("delete destination %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}

func s3TestResponse(w http.ResponseWriter, err error) {
	if err != nil {
		jsonOut(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTestDestinationPayload(w http.ResponseWriter, r *http.Request) {
	var p destinationPayload
	if !decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(true); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	s3, err := storage.NewS3(storage.S3Config{
		Endpoint: p.Endpoint, Region: p.Region, Bucket: p.Bucket,
		Prefix: p.Prefix, AccessKey: p.AccessKey, SecretKey: p.SecretKey,
	})
	if err != nil {
		s3TestResponse(w, err)
		return
	}
	s3TestResponse(w, s3.Test(r.Context()))
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
	secret, err := s.Crypt.Decrypt(d.SecretEnc)
	if err != nil {
		log.Printf("decrypt secret dest %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	s3, err := storage.NewS3(storage.S3Config{
		Endpoint: d.Endpoint, Region: d.Region, Bucket: d.Bucket,
		Prefix: d.Prefix, AccessKey: d.AccessKey, SecretKey: secret,
	})
	if err != nil {
		s3TestResponse(w, err)
		return
	}
	s3TestResponse(w, s3.Test(r.Context()))
}
