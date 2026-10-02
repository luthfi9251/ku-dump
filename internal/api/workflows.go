package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
)

type workflowDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DatabaseID   string `json:"databaseId"`
	DatabaseName string `json:"databaseName"`
	Engine       string `json:"engine"`
	DestID       string `json:"destId"`
	DestName     string `json:"destName"`
	TriggerKind  string `json:"triggerKind"`
	RunAt        string `json:"runAt"`
	Cron         string `json:"cron"`
	Enabled      bool   `json:"enabled"`
	LastRunAt    string `json:"lastRunAt"`
	LastError    string `json:"lastError"`
	NextRunAt    string `json:"nextRunAt"`
	CreatedAt    string `json:"createdAt"`
}

func timePtrRFC3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func workflowToDTO(r meta.WorkflowRow) workflowDTO {
	return workflowDTO{
		ID: encID(r.ID), Name: r.Name,
		DatabaseID: encID(r.DatabaseID), DatabaseName: r.DatabaseName, Engine: r.DatabaseEngine,
		DestID: encIDOrEmpty(r.DestID), DestName: r.DestName,
		TriggerKind: r.TriggerKind, RunAt: timePtrRFC3339(r.RunAt), Cron: r.Cron,
		Enabled: r.Enabled, LastRunAt: timePtrRFC3339(r.LastRunAt),
		LastError: r.LastError, NextRunAt: timePtrRFC3339(r.NextRunAt),
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
}

type workflowPayload struct {
	Name        string `json:"name"`
	DatabaseID  string `json:"databaseId"`
	DestID      string `json:"destId"`
	TriggerKind string `json:"triggerKind"`
	RunAt       string `json:"runAt"`
	Cron        string `json:"cron"`
	Enabled     *bool  `json:"enabled"`
}

// validateWorkflowTrigger returns run_at, cron and next_run_at for the given
// trigger kind. next_run_at: manual -> nil, once -> run_at, cron -> Next(now).
func validateWorkflowTrigger(kind, runAt, cronExpr string, now time.Time) (*time.Time, string, *time.Time, error) {
	switch kind {
	case "manual":
		return nil, "", nil, nil
	case "once":
		if runAt == "" {
			return nil, "", nil, errors.New("runAt is required for once triggers")
		}
		t, err := time.Parse(time.RFC3339, runAt)
		if err != nil {
			return nil, "", nil, errors.New("runAt must be an RFC3339 timestamp")
		}
		if !t.After(now) {
			return nil, "", nil, errors.New("runAt must be in the future")
		}
		return &t, "", &t, nil
	case "cron":
		sched, err := cron.ParseStandard(cronExpr)
		if err != nil {
			return nil, "", nil, fmt.Errorf("invalid cron: %v", err)
		}
		t := sched.Next(now)
		return nil, cronExpr, &t, nil
	default:
		return nil, "", nil, errors.New("triggerKind must be manual, once or cron")
	}
}

func (s *Server) getWorkflowOr404(w http.ResponseWriter, r *http.Request) (*meta.Workflow, bool) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return nil, false
	}
	wf, err := s.Store.GetWorkflow(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return nil, false
	}
	return wf, true
}

func (s *Server) resolveWorkflowRefs(w http.ResponseWriter, r *http.Request, p workflowPayload) (dbID, destID int64, ok bool) {
	dbID, err := decID(p.DatabaseID)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return 0, 0, false
	}
	if _, err := s.Store.GetDatabase(r.Context(), dbID); err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return 0, 0, false
	}
	if p.DestID != "" {
		destID, err = decID(p.DestID)
		if err != nil {
			fail(w, http.StatusBadRequest, "VALIDATION", "invalid destId")
			return 0, 0, false
		}
		if _, err := s.Store.GetDestination(r.Context(), destID); err != nil {
			fail(w, http.StatusBadRequest, "STORAGE_NOT_CONFIGURED", "storage destination not found")
			return 0, 0, false
		}
	}
	return dbID, destID, true
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListWorkflows(r.Context())
	if err != nil {
		log.Printf("list workflows: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	out := make([]workflowDTO, 0, len(rows))
	for i := range rows {
		out = append(out, workflowToDTO(rows[i]))
	}
	jsonOut(w, http.StatusOK, out)
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var p workflowPayload
	if !decodeBody(w, r, &p) {
		return
	}
	dbID, destID, ok := s.resolveWorkflowRefs(w, r, p)
	if !ok {
		return
	}
	runAt, cronExpr, next, err := validateWorkflowTrigger(p.TriggerKind, p.RunAt, p.Cron, time.Now())
	if err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	db, _ := s.Store.GetDatabase(r.Context(), dbID)
	name := p.Name
	if name == "" {
		name = db.Name + " (" + db.Engine + ")"
	}
	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	wfID, err := s.Store.CreateWorkflow(r.Context(), &meta.Workflow{
		Name: name, DatabaseID: dbID, DestID: destID,
		TriggerKind: p.TriggerKind, RunAt: runAt, Cron: cronExpr,
		Enabled: enabled, NextRunAt: next,
	})
	if err != nil {
		log.Printf("create workflow: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	row, err := s.Store.GetWorkflowRow(r.Context(), wfID)
	if err != nil {
		log.Printf("get workflow row: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusCreated, workflowToDTO(*row))
}

func (s *Server) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	current, ok := s.getWorkflowOr404(w, r)
	if !ok {
		return
	}
	var p workflowPayload
	if !decodeBody(w, r, &p) {
		return
	}
	dbID, destID, ok := s.resolveWorkflowRefs(w, r, p)
	if !ok {
		return
	}
	runAt, cronExpr, next, err := validateWorkflowTrigger(p.TriggerKind, p.RunAt, p.Cron, time.Now())
	if err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	enabled := current.Enabled
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	name := p.Name
	if name == "" {
		db, _ := s.Store.GetDatabase(r.Context(), dbID)
		name = db.Name + " (" + db.Engine + ")"
	}
	updated := *current
	updated.Name = name
	updated.DatabaseID = dbID
	updated.DestID = destID
	updated.TriggerKind = p.TriggerKind
	updated.RunAt = runAt
	updated.Cron = cronExpr
	updated.Enabled = enabled
	updated.NextRunAt = next
	if err := s.Store.UpdateWorkflow(r.Context(), &updated); err != nil {
		log.Printf("update workflow %d: %v", current.ID, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	row, err := s.Store.GetWorkflowRow(r.Context(), current.ID)
	if err != nil {
		log.Printf("get workflow row: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusOK, workflowToDTO(*row))
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	wf, ok := s.getWorkflowOr404(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteWorkflow(r.Context(), wf.ID); err != nil {
		log.Printf("delete workflow %d: %v", wf.ID, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRunWorkflow(w http.ResponseWriter, r *http.Request) {
	wf, ok := s.getWorkflowOr404(w, r)
	if !ok {
		return
	}
	jobID, dumpID, err := s.Runner.StartDump(r.Context(), wf.DatabaseID, wf.DestID, wf.Name, wf.ID, userID(r))
	if err != nil {
		switch {
		case errors.Is(err, meta.ErrJobActive):
			fail(w, http.StatusConflict, "JOB_ACTIVE", "a job is already running for this database")
		case errors.Is(err, runner.ErrToolsMissing):
			fail(w, http.StatusBadRequest, "TOOL_MISSING", err.Error())
		case errors.Is(err, runner.ErrStorageUnavailable):
			fail(w, http.StatusBadRequest, "STORAGE_NOT_CONFIGURED", err.Error())
		case errors.Is(err, meta.ErrNotFound):
			fail(w, http.StatusNotFound, "NOT_FOUND", "workflow target not found")
		default:
			log.Printf("run workflow %d: %v", wf.ID, err)
			fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		}
		return
	}
	// manual run bookkeeping: set last_run_at, clear last_error; enabled and
	// next_run_at passed through unchanged so the schedule never shifts.
	now := time.Now()
	if err := s.Store.SetWorkflowState(r.Context(), wf.ID, &now, "", wf.Enabled, wf.NextRunAt); err != nil {
		log.Printf("workflow %d state: %v", wf.ID, err)
	}
	jsonOut(w, http.StatusCreated, map[string]string{"jobId": encID(jobID), "dumpId": encID(dumpID)})
}
