package api

import (
	"net/http"

	"github.com/luthfi9251/ku-dump/internal/meta"
)

type restoreRequest struct {
	DumpID           string `json:"dumpId"`
	TargetDatabaseID string `json:"targetDatabaseId"`
	ConfirmName      string `json:"confirmName"`
}

func (s *Server) handleCreateRestore(w http.ResponseWriter, r *http.Request) {
	var req restoreRequest
	if !decodeBody(w, r, &req) {
		return
	}
	dumpID, err := decID(req.DumpID)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "dump not found")
		return
	}
	dump, err := s.Store.GetDump(r.Context(), dumpID)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "dump not found")
		return
	}
	if dump.Status != "ready" && dump.Status != "uploaded" {
		fail(w, http.StatusConflict, "DUMP_NOT_READY", "dump is not ready (status: "+dump.Status+")")
		return
	}
	targetID, err := decID(req.TargetDatabaseID)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "target database not found")
		return
	}
	target, err := s.Store.GetDatabase(r.Context(), targetID)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "target database not found")
		return
	}
	if dump.Engine != target.Engine {
		fail(w, http.StatusBadRequest, "ENGINE_MISMATCH",
			"dump engine ("+dump.Engine+") does not match target database engine ("+target.Engine+")")
		return
	}
	if req.ConfirmName != target.Name {
		fail(w, http.StatusBadRequest, "CONFIRM_MISMATCH",
			"type the target database name ("+target.Name+") to confirm the restore")
		return
	}
	if has, _ := s.Store.HasActiveJob(r.Context(), target.ID); has {
		fail(w, http.StatusConflict, "JOB_ACTIVE", "a job is already running for the target database")
		return
	}
	jobID, err := s.Store.CreateJob(r.Context(), &meta.Job{
		Type: "restore", DatabaseID: target.ID, DumpID: &dump.ID,
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	s.Runner.Start(jobID)
	jsonOut(w, http.StatusCreated, map[string]string{"jobId": encID(jobID)})
}
