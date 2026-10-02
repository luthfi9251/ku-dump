package api

import (
	"io"
	"log"
	"net/http"
	"path/filepath"
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
	Storage      string `json:"storageKind"`
	SizeBytes    int64  `json:"sizeBytes"`
	SourceDB     string `json:"sourceDb"`
	CreatedBy    string `json:"createdBy"`
	CreatedAt    string `json:"createdAt"`
	WorkflowName string `json:"workflowName"`
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
		Storage:      r.Storage,
		SizeBytes:    r.SizeBytes,
		SourceDB:     r.SourceDB,
		CreatedBy:    encIDOrEmpty(r.CreatedBy),
		CreatedAt:    r.CreatedAt.Format(time.RFC3339),
		WorkflowName: r.WorkflowName,
	}
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
