package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/luthfi9251/ku-dump/internal/meta"
)

type databasePayload struct {
	Name     string `json:"name"`
	Engine   string `json:"engine"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"dbName"`
	Username string `json:"username"`
	Password string `json:"password"`
	Options  string `json:"options"`
}

func (p databasePayload) validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name is required")
	}
	if p.Engine != "postgres" && p.Engine != "mongodb" {
		return errors.New("engine must be postgres or mongodb")
	}
	if p.Host == "" || p.Port < 1 || p.Port > 65535 || p.DBName == "" || p.Username == "" {
		return errors.New("host, port (1-65535), dbName and username are required")
	}
	if p.Options != "" && !json.Valid([]byte(p.Options)) {
		return errors.New("options must be valid JSON")
	}
	return nil
}

func (p databasePayload) toMeta(passwordEnc string) meta.Database {
	return meta.Database{
		Name: p.Name, Engine: p.Engine, Host: p.Host, Port: p.Port,
		DBName: p.DBName, Username: p.Username, PasswordEnc: passwordEnc, Options: p.Options,
	}
}

type databaseDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Engine       string   `json:"engine"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	DBName       string   `json:"dbName"`
	Username     string   `json:"username"`
	Options      string   `json:"options"`
	LastTestAt   *string  `json:"lastTestAt"`
	LastTestOK   *bool    `json:"lastTestOk"`
	HasActiveJob bool     `json:"hasActiveJob"`
	ToolsMissing []string `json:"toolsMissing"`
}

func timePtrStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func (s *Server) dbDTO(ctx context.Context, db *meta.Database) databaseDTO {
	has, _ := s.Store.HasActiveJob(ctx, db.ID)
	missing := []string{}
	if s.Engines[db.Engine] != nil {
		missing = s.Engines[db.Engine].ToolsMissing()
	}
	return databaseDTO{
		ID: encID(db.ID), Name: db.Name, Engine: db.Engine, Host: db.Host, Port: db.Port,
		DBName: db.DBName, Username: db.Username, Options: db.Options,
		LastTestAt: timePtrStr(db.LastTestAt), LastTestOK: db.LastTestOK,
		HasActiveJob: has, ToolsMissing: missing,
	}
}

func (s *Server) handleListDatabases(w http.ResponseWriter, r *http.Request) {
	dbs, err := s.Store.ListDatabases(r.Context())
	if err != nil {
		log.Printf("list databases: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	out := make([]databaseDTO, 0, len(dbs))
	for i := range dbs {
		out = append(out, s.dbDTO(r.Context(), &dbs[i]))
	}
	jsonOut(w, http.StatusOK, out)
}

func (s *Server) handleCreateDatabase(w http.ResponseWriter, r *http.Request) {
	var p databasePayload
	if !decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if p.Password == "" {
		fail(w, http.StatusBadRequest, "VALIDATION", "password is required")
		return
	}
	if err := s.checkNameFree(r.Context(), p.Name, 0); err != nil {
		fail(w, http.StatusConflict, "DUPLICATE_NAME", "database name already registered")
		return
	}
	eng := s.Engines[p.Engine]
	if eng == nil {
		fail(w, http.StatusBadRequest, "VALIDATION", "unknown engine")
		return
	}
	if missing := eng.ToolsMissing(); len(missing) > 0 {
		fail(w, http.StatusBadRequest, "TOOL_MISSING", "missing tools: "+strings.Join(missing, ", "))
		return
	}
	enc, err := s.Crypt.Encrypt(p.Password)
	if err != nil {
		log.Printf("encrypt password: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	db := p.toMeta(enc)
	if err := eng.TestConnection(r.Context(), db); err != nil {
		fail(w, http.StatusBadRequest, "CONNECTION_FAILED", err.Error())
		return
	}
	now := time.Now()
	ok := true
	db.LastTestAt = &now
	db.LastTestOK = &ok
	id, err := s.Store.CreateDatabase(r.Context(), &db)
	if err != nil {
		log.Printf("create database: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	created, _ := s.Store.GetDatabase(r.Context(), id)
	jsonOut(w, http.StatusCreated, s.dbDTO(r.Context(), created))
}

func (s *Server) checkNameFree(ctx context.Context, name string, excludeID int64) error {
	existing, err := s.Store.GetDatabaseByName(ctx, name)
	if err != nil {
		return nil
	}
	if existing.ID != excludeID {
		return errors.New("duplicate")
	}
	return nil
}

func (s *Server) handleUpdateDatabase(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return
	}
	current, err := s.Store.GetDatabase(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return
	}
	var p databasePayload
	if !decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if err := s.checkNameFree(r.Context(), p.Name, id); err != nil {
		fail(w, http.StatusConflict, "DUPLICATE_NAME", "database name already registered")
		return
	}
	eng := s.Engines[p.Engine]
	if eng == nil {
		fail(w, http.StatusBadRequest, "VALIDATION", "unknown engine")
		return
	}
	if missing := eng.ToolsMissing(); len(missing) > 0 {
		fail(w, http.StatusBadRequest, "TOOL_MISSING", "missing tools: "+strings.Join(missing, ", "))
		return
	}
	enc := current.PasswordEnc
	if p.Password != "" {
		enc, err = s.Crypt.Encrypt(p.Password)
		if err != nil {
			log.Printf("encrypt password: %v", err)
			fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
	}
	db := p.toMeta(enc)
	db.ID = id
	if err := eng.TestConnection(r.Context(), db); err != nil {
		fail(w, http.StatusBadRequest, "CONNECTION_FAILED", err.Error())
		return
	}
	now := time.Now()
	ok := true
	db.LastTestAt = &now
	db.LastTestOK = &ok
	if err := s.Store.UpdateDatabase(r.Context(), &db); err != nil {
		log.Printf("update database %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	updated, _ := s.Store.GetDatabase(r.Context(), id)
	jsonOut(w, http.StatusOK, s.dbDTO(r.Context(), updated))
}

func (s *Server) handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return
	}
	if _, err := s.Store.GetDatabase(r.Context(), id); err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return
	}
	if err := s.Store.DeleteDatabase(r.Context(), id); err != nil {
		log.Printf("delete database %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTestDatabase(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return
	}
	db, err := s.Store.GetDatabase(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return
	}
	err = s.Engines[db.Engine].TestConnection(r.Context(), *db)
	if err == nil {
		_ = s.Store.SetDatabaseTestResult(r.Context(), id, true)
		jsonOut(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	_ = s.Store.SetDatabaseTestResult(r.Context(), id, false)
	jsonOut(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
}

func (s *Server) handleTestDatabasePayload(w http.ResponseWriter, r *http.Request) {
	var p databasePayload
	if !decodeBody(w, r, &p) {
		return
	}
	if p.Engine != "postgres" && p.Engine != "mongodb" {
		fail(w, http.StatusBadRequest, "VALIDATION", "engine must be postgres or mongodb")
		return
	}
	if p.Host == "" || p.DBName == "" {
		fail(w, http.StatusBadRequest, "VALIDATION", "host and dbName are required")
		return
	}
	eng := s.Engines[p.Engine]
	if eng == nil {
		fail(w, http.StatusBadRequest, "VALIDATION", "unknown engine")
		return
	}
	if missing := eng.ToolsMissing(); len(missing) > 0 {
		fail(w, http.StatusBadRequest, "TOOL_MISSING", "missing tools: "+strings.Join(missing, ", "))
		return
	}
	enc, err := s.Crypt.Encrypt(p.Password)
	if err != nil {
		log.Printf("encrypt password: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	err = eng.TestConnection(r.Context(), p.toMeta(enc))
	if err == nil {
		jsonOut(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
}
