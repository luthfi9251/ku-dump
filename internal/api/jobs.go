package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
)

type jobDTO struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	DatabaseID   string  `json:"databaseId"`
	DatabaseName string  `json:"databaseName"`
	DumpID       string  `json:"dumpId"`
	DumpLabel    string  `json:"dumpLabel"`
	Storage      *string `json:"storage"`
	Status       string  `json:"status"`
	Error        string  `json:"error"`
	LogPath      string  `json:"logPath"`
	CreatedAt    string  `json:"createdAt"`
	StartedAt    *string `json:"startedAt"`
	FinishedAt   *string `json:"finishedAt"`
	LogTail      string  `json:"logTail,omitempty"`
}

func jobToDTO(r meta.JobRow) jobDTO {
	return jobDTO{
		ID:           encID(r.ID),
		Type:         r.Type,
		DatabaseID:   encID(r.DatabaseID),
		DatabaseName: r.DatabaseName,
		DumpID:       encIDOrEmpty(derefI64(r.DumpID)),
		DumpLabel:    r.DumpLabel,
		Storage:      r.Storage,
		Status:       r.Status,
		Error:        r.Err,
		LogPath:      r.LogPath,
		CreatedAt:    r.CreatedAt.Format(time.RFC3339),
		StartedAt:    timePtrStr(r.StartedAt),
		FinishedAt:   timePtrStr(r.FinishedAt),
	}
}

func derefI64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.Store.ListJobs(r.Context(), limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	out := make([]jobDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, jobToDTO(row))
	}
	jsonOut(w, http.StatusOK, out)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "job not found")
		return
	}
	job, err := s.Store.GetJob(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "job not found")
		return
	}
	row := meta.JobRow{Job: *job}
	if job.DatabaseID != 0 {
		if db, err := s.Store.GetDatabase(r.Context(), job.DatabaseID); err == nil {
			row.DatabaseName = db.Name
		}
	}
	if job.DumpID != nil {
		if d, err := s.Store.GetDump(r.Context(), *job.DumpID); err == nil {
			row.DumpLabel = d.Label
		}
	}
	dto := jobToDTO(row)
	dto.LogTail = runner.TailFile(job.LogPath, 8192)
	jsonOut(w, http.StatusOK, dto)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "job not found")
		return
	}
	job, err := s.Store.GetJob(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "job not found")
		return
	}
	switch job.Status {
	case "pending", "running":
	default:
		fail(w, http.StatusConflict, "NOT_CANCELLABLE", "job already finished: "+job.Status)
		return
	}
	if !s.Runner.Cancel(id) {
		fail(w, http.StatusConflict, "NOT_CANCELLABLE", "job is no longer running")
		return
	}
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}
