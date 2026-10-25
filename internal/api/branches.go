package api

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/luthfi9251/ku-dump/internal/meta"
)

type renameBranchRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleRenameBranch(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "branch not found")
		return
	}
	var req renameBranchRequest
	if !decodeBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 60 {
		fail(w, http.StatusBadRequest, "VALIDATION", "branch name must be 1 to 60 characters")
		return
	}
	if _, err := s.Store.GetBranch(r.Context(), id); err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "branch not found")
		return
	}
	if err := s.Store.RenameBranch(r.Context(), id, name); err != nil {
		if errors.Is(err, meta.ErrBranchNameTaken) {
			fail(w, http.StatusConflict, "NAME_TAKEN", "another branch of this database is already called "+name)
			return
		}
		log.Printf("rename branch %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusOK, map[string]string{"id": encID(id), "name": name})
}

type restorePreviewDTO struct {
	Action     string `json:"action"` // continue | new
	BranchID   string `json:"branchId,omitempty"`
	BranchName string `json:"branchName,omitempty"`
}

// handleRestorePreview tells whether restoring a dump into a database would
// continue one of its branches or start a new one.
func (s *Server) handleRestorePreview(w http.ResponseWriter, r *http.Request) {
	dumpID, err1 := decID(r.URL.Query().Get("dumpId"))
	dbID, err2 := decID(r.URL.Query().Get("targetDatabaseId"))
	if err1 != nil || err2 != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", "dumpId and targetDatabaseId are required")
		return
	}
	b, err := s.Store.PreviewRestoreBranch(r.Context(), dumpID, dbID)
	if err != nil {
		log.Printf("restore preview: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if b == nil {
		jsonOut(w, http.StatusOK, restorePreviewDTO{Action: "new"})
		return
	}
	jsonOut(w, http.StatusOK, restorePreviewDTO{Action: "continue", BranchID: encID(b.ID), BranchName: b.Name})
}
