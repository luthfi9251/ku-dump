package api

import (
	"context"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/luthfi9251/ku-dump/internal/meta"
)

// The dump graph: one lane per database, dump and restore events as nodes.
// A dump points at its parent (the previous dump of the same database, or the
// restore that last replaced the database's contents); a restore points at
// its source dump. See meta/lineage.go for how parents are recorded.

type graphLaneDTO struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // database | deleted | upload
	DatabaseID string `json:"databaseId"`
	Name       string `json:"name"`
}

type graphNodeDTO struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"` // dump | restore
	Lane          string   `json:"lane"`
	At            string   `json:"at"`
	Status        string   `json:"status"`
	Parent        string   `json:"parent,omitempty"`
	Source        string   `json:"source,omitempty"`
	SourceDeleted bool     `json:"sourceDeleted,omitempty"`
	JobID         string   `json:"jobId,omitempty"`
	FinishedAt    string   `json:"finishedAt,omitempty"`
	Dump          *dumpDTO `json:"dump,omitempty"`
}

type graphDTO struct {
	Engine string         `json:"engine"`
	Lanes  []graphLaneDTO `json:"lanes"`
	Nodes  []graphNodeDTO `json:"nodes"`
}

func dumpNodeID(id int64) string    { return "d-" + encID(id) }
func restoreNodeID(id int64) string { return "r-" + encID(id) }
func laneID(dbID int64) string      { return "db-" + encID(dbID) }

func (s *Server) handleDumpGraph(w http.ResponseWriter, r *http.Request) {
	engine := r.URL.Query().Get("engine")
	if engine == "" {
		engine = "postgres"
	}
	if engine != "postgres" && engine != "mongodb" {
		fail(w, http.StatusBadRequest, "VALIDATION", "engine must be postgres or mongodb")
		return
	}
	g, err := s.buildDumpGraph(r.Context(), engine)
	if err != nil {
		log.Printf("dump graph: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusOK, g)
}

func (s *Server) buildDumpGraph(ctx context.Context, engine string) (*graphDTO, error) {
	dbs, err := s.Store.ListDatabases(ctx)
	if err != nil {
		return nil, err
	}
	allDumps, err := s.Store.ListDumps(ctx)
	if err != nil {
		return nil, err
	}
	restores, err := s.Store.ListRestores(ctx)
	if err != nil {
		return nil, err
	}

	out := &graphDTO{Engine: engine, Lanes: []graphLaneDTO{}, Nodes: []graphNodeDTO{}}
	lanes := map[string]bool{}
	for _, db := range dbs {
		if db.Engine != engine {
			continue
		}
		lanes[laneID(db.ID)] = true
		out.Lanes = append(out.Lanes, graphLaneDTO{ID: laneID(db.ID), Kind: "database", DatabaseID: encID(db.ID), Name: db.Name})
	}
	// events of databases that were deleted since keep a lane of their own
	laneFor := func(dbID int64, name string) string {
		id := laneID(dbID)
		if !lanes[id] {
			lanes[id] = true
			if name == "" {
				name = "deleted database"
			}
			out.Lanes = append(out.Lanes, graphLaneDTO{ID: id, Kind: "deleted", Name: name})
		}
		return id
	}

	// failed dumps never became a database state, so they stay out of the graph
	var dumps []meta.DumpRow
	inGraph := map[int64]bool{}
	for _, d := range allDumps {
		if d.Engine == engine && d.Status != "failed" {
			dumps = append(dumps, d)
			inGraph[d.ID] = true
		}
	}

	type event struct {
		at   time.Time
		node graphNodeDTO
	}
	var events []event
	restoreIDs := map[int64]bool{}
	for _, rs := range restores {
		if rs.Engine != engine {
			continue
		}
		restoreIDs[rs.JobID] = true
		n := graphNodeDTO{
			ID: restoreNodeID(rs.JobID), Kind: "restore", Lane: laneFor(rs.DatabaseID, rs.DatabaseName),
			At: rs.CreatedAt.Format(time.RFC3339), Status: rs.Status, JobID: encID(rs.JobID),
		}
		if rs.FinishedAt != nil {
			n.FinishedAt = rs.FinishedAt.Format(time.RFC3339)
		}
		if rs.DumpExists && inGraph[rs.DumpID] {
			n.Source = dumpNodeID(rs.DumpID)
		} else {
			n.SourceDeleted = true
		}
		events = append(events, event{rs.CreatedAt, n})
	}
	hasUploads := false
	for _, d := range dumps {
		dto := dumpToDTO(d)
		n := graphNodeDTO{ID: dumpNodeID(d.ID), Kind: "dump", At: dto.CreatedAt, Status: d.Status, Dump: &dto}
		if d.DatabaseID == 0 {
			n.Lane = "upload"
			hasUploads = true
		} else {
			n.Lane = laneFor(d.DatabaseID, d.DatabaseName)
		}
		switch {
		case d.ParentJobID != 0 && restoreIDs[d.ParentJobID]:
			n.Parent = restoreNodeID(d.ParentJobID)
		case d.ParentJobID == 0 && d.ParentDumpID != 0 && inGraph[d.ParentDumpID]:
			n.Parent = dumpNodeID(d.ParentDumpID)
		}
		events = append(events, event{d.CreatedAt, n})
	}
	if hasUploads {
		out.Lanes = append(out.Lanes, graphLaneDTO{ID: "upload", Kind: "upload", Name: "uploaded files"})
	}

	// Order by time; timestamps have one-second resolution, so ties are
	// broken by emitting each node after the node it points at.
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.Before(events[j].at) })
	byID := map[string]graphNodeDTO{}
	for _, e := range events {
		byID[e.node.ID] = e.node
	}
	emitted := map[string]bool{}
	var emit func(n graphNodeDTO)
	emit = func(n graphNodeDTO) {
		if emitted[n.ID] {
			return
		}
		emitted[n.ID] = true
		for _, dep := range []string{n.Parent, n.Source} {
			if d, ok := byID[dep]; ok {
				emit(d)
			}
		}
		out.Nodes = append(out.Nodes, n)
	}
	for _, e := range events {
		emit(e.node)
	}
	return out, nil
}
