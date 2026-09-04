package api

import (
	"errors"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/luthfi9251/ku-dump/internal/meta"
)

type dumpDTO struct {
	ID           string `json:"id"`
	DatabaseID   string `json:"databaseId"`
	DatabaseName string `json:"databaseName"`
	Engine       string `json:"engine"`
	Label        string `json:"label"`
	DestID       string `json:"destId"`
	DestName     string `json:"destName"`
	Status       string `json:"status"`
	SizeBytes    int64  `json:"sizeBytes"`
	SourceDB     string `json:"sourceDb"`
	CreatedBy    string `json:"createdBy"`
	CreatedAt    string `json:"createdAt"`
}

func encIDOrEmpty(id int64) string {
	if id == 0 {
		return ""
	}
	return encID(id)
}

func dumpToDTO(r meta.DumpRow) dumpDTO {
	return dumpDTO{
		ID:           encID(r.ID),
		DatabaseID:   encIDOrEmpty(r.DatabaseID),
		DatabaseName: r.DatabaseName,
		Engine:       r.Engine,
		Label:        r.Label,
		DestID:       encIDOrEmpty(r.DestID),
		DestName:     r.DestName,
		Status:       r.Status,
		SizeBytes:    r.SizeBytes,
		SourceDB:     r.SourceDB,
		CreatedBy:    encIDOrEmpty(r.CreatedBy),
		CreatedAt:    r.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleCreateDump(w http.ResponseWriter, r *http.Request) {
	dbID, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return
	}
	db, err := s.Store.GetDatabase(r.Context(), dbID)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return
	}
	var req struct {
		Label  string `json:"label"`
		DestID string `json:"destId"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	destID := int64(0)
	storageKind := "local"
	if req.DestID != "" {
		var err error
		destID, err = decID(req.DestID)
		if err != nil {
			fail(w, http.StatusBadRequest, "VALIDATION", "invalid destId")
			return
		}
		dest, err := s.Store.GetDestination(r.Context(), destID)
		if err != nil {
			fail(w, http.StatusBadRequest, "STORAGE_NOT_CONFIGURED", "storage destination not found")
			return
		}
		storageKind = dest.Kind
	}
	if _, err := s.NewStore(r.Context(), destID); err != nil {
		fail(w, http.StatusBadRequest, "STORAGE_NOT_CONFIGURED", "selected storage is not available: "+err.Error())
		return
	}
	eng := s.Engines[db.Engine]
	if missing := eng.ToolsMissing(); len(missing) > 0 {
		fail(w, http.StatusBadRequest, "TOOL_MISSING", "missing tools: "+strings.Join(missing, ", "))
		return
	}
	label := req.Label
	if label == "" {
		label = db.Name + " " + time.Now().Format("2006-01-02 15:04")
	}
	dumpID, err := s.Store.CreateDump(r.Context(), &meta.Dump{
		DatabaseID: db.ID, Engine: db.Engine, Label: label, Storage: storageKind,
		DestID: destID, SourceDB: db.DBName, Status: "pending", CreatedBy: userID(r),
	})
	if err != nil {
		log.Printf("create dump: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jobID, err := s.Store.CreateJobGuarded(r.Context(), &meta.Job{
		Type: "dump", DatabaseID: db.ID, DumpID: &dumpID,
	})
	if err != nil {
		if errors.Is(err, meta.ErrJobActive) {
			_ = s.Store.DeleteDump(r.Context(), dumpID)
			fail(w, http.StatusConflict, "JOB_ACTIVE", "a job is already running for this database")
			return
		}
		log.Printf("create dump job: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	s.Runner.Start(jobID)
	jsonOut(w, http.StatusCreated, map[string]string{"jobId": encID(jobID), "dumpId": encID(dumpID)})
}

func (s *Server) handleListDumps(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListDumps(r.Context())
	if err != nil {
		log.Printf("list dumps: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	engineFilter := r.URL.Query().Get("engine")
	dbFilter := r.URL.Query().Get("databaseId")
	var dbFilterID int64
	if dbFilter != "" {
		dbFilterID, _ = decID(dbFilter)
	}
	out := make([]dumpDTO, 0, len(rows))
	for _, row := range rows {
		if engineFilter != "" && row.Engine != engineFilter {
			continue
		}
		if dbFilter != "" && row.DatabaseID != dbFilterID {
			continue
		}
		out = append(out, dumpToDTO(row))
	}
	jsonOut(w, http.StatusOK, out)
}

func (s *Server) handleDownloadDump(w http.ResponseWriter, r *http.Request) {
	dump, ok := s.getDumpOr404(w, r)
	if !ok {
		return
	}
	if dump.Status != "ready" && dump.Status != "uploaded" {
		fail(w, http.StatusConflict, "NOT_READY", "dump is not ready for download")
		return
	}
	store, err := s.NewStore(r.Context(), dump.DestID)
	if err != nil {
		log.Printf("open storage %d: %v", dump.DestID, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	rc, err := store.Open(r.Context(), dump.Location)
	if err != nil {
		log.Printf("open dump %d: %v", dump.ID, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(dump.Location)+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, rc)
}

func (s *Server) handleDeleteDump(w http.ResponseWriter, r *http.Request) {
	dump, ok := s.getDumpOr404(w, r)
	if !ok {
		return
	}
	if dump.Status == "pending" {
		fail(w, http.StatusConflict, "NOT_READY", "dump is still in progress")
		return
	}
	if store, err := s.NewStore(r.Context(), dump.DestID); err == nil {
		_ = store.Delete(r.Context(), dump.Location)
	}
	if err := s.Store.DeleteDump(r.Context(), dump.ID); err != nil {
		log.Printf("delete dump %d: %v", dump.ID, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) getDumpOr404(w http.ResponseWriter, r *http.Request) (*meta.Dump, bool) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "dump not found")
		return nil, false
	}
	dump, err := s.Store.GetDump(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "dump not found")
		return nil, false
	}
	return dump, true
}
