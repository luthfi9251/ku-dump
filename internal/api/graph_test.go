package api

import (
	"encoding/json"
	"testing"
)

func TestDumpGraph(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	prod := createDatabase(t, h, cookie)
	dev := createTargetDB(t, h, cookie, "dev")

	_, p1 := dumpViaWorkflow(t, h, cookie, prod.ID, "prod nightly")
	rec := doJSON(t, h, "POST", "/api/restores", restorePayload{DumpID: p1.ID, TargetDatabaseID: dev.ID, ConfirmName: "dev"}, cookie)
	if rec.Code != 201 {
		t.Fatalf("restore = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		JobID string `json:"jobId"`
	}
	json.NewDecoder(rec.Body).Decode(&created)
	if j := waitJobTerminal(t, h, cookie, created.JobID); j.Status != "success" {
		t.Fatalf("restore job = %+v", j)
	}
	_, a1 := dumpViaWorkflow(t, h, cookie, dev.ID, "dev snapshot")

	rec = doJSON(t, h, "GET", "/api/dumps/graph?engine=postgres", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("graph = %d: %s", rec.Code, rec.Body.String())
	}
	var g graphDTO
	if err := json.NewDecoder(rec.Body).Decode(&g); err != nil {
		t.Fatal(err)
	}
	if len(g.Lanes) != 2 {
		t.Fatalf("lanes = %+v", g.Lanes)
	}
	if len(g.Nodes) != 3 {
		t.Fatalf("nodes = %+v", g.Nodes)
	}
	byID := map[string]graphNodeDTO{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	restore := byID["r-"+created.JobID]
	if restore.Kind != "restore" || restore.Source != "d-"+p1.ID || restore.Lane != "db-"+dev.ID || restore.Status != "success" {
		t.Fatalf("restore node = %+v", restore)
	}
	devDump := byID["d-"+a1.ID]
	if devDump.Parent != restore.ID || devDump.Dump == nil || devDump.Dump.Label != "dev snapshot" {
		t.Fatalf("dev dump node = %+v", devDump)
	}
	if byID["d-"+p1.ID].Parent != "" {
		t.Fatalf("prod dump should be a root: %+v", byID["d-"+p1.ID])
	}
	// nodes come in event order
	if g.Nodes[0].ID != "d-"+p1.ID || g.Nodes[1].ID != restore.ID || g.Nodes[2].ID != devDump.ID {
		t.Fatalf("order = %s %s %s", g.Nodes[0].ID, g.Nodes[1].ID, g.Nodes[2].ID)
	}

	// branches: prod has one, the restore into dev starts dev's first branch
	if len(g.Branches) != 2 {
		t.Fatalf("branches = %+v", g.Branches)
	}
	if restore.Branch == "" || devDump.Branch != restore.Branch || byID["d-"+p1.ID].Branch == restore.Branch {
		t.Fatalf("branch ids: prod %q restore %q dev dump %q", byID["d-"+p1.ID].Branch, restore.Branch, devDump.Branch)
	}
	for _, b := range g.Branches {
		if !b.Active || b.Name == "" {
			t.Fatalf("branch = %+v", b)
		}
	}

	// preview: the tip of dev's branch continues it, a prod dump starts a new one
	var pv restorePreviewDTO
	rec = doJSON(t, h, "GET", "/api/restores/preview?dumpId="+a1.ID+"&targetDatabaseId="+dev.ID, nil, cookie)
	json.NewDecoder(rec.Body).Decode(&pv)
	if rec.Code != 200 || pv.Action != "continue" || pv.BranchID != restore.Branch {
		t.Fatalf("preview tip = %d %+v", rec.Code, pv)
	}
	pv = restorePreviewDTO{}
	rec = doJSON(t, h, "GET", "/api/restores/preview?dumpId="+p1.ID+"&targetDatabaseId="+dev.ID, nil, cookie)
	json.NewDecoder(rec.Body).Decode(&pv)
	if pv.Action != "new" {
		t.Fatalf("preview foreign dump = %+v", pv)
	}

	// rename
	rec = doJSON(t, h, "PUT", "/api/branches/"+restore.Branch, map[string]string{"name": "feature-x"}, cookie)
	if rec.Code != 200 {
		t.Fatalf("rename = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, "PUT", "/api/branches/"+restore.Branch, map[string]string{"name": "  "}, cookie)
	if rec.Code != 400 {
		t.Fatalf("empty rename = %d", rec.Code)
	}
	rec = doJSON(t, h, "PUT", "/api/branches/"+encID(999), map[string]string{"name": "x"}, cookie)
	if rec.Code != 404 {
		t.Fatalf("unknown branch rename = %d", rec.Code)
	}

	rec = doJSON(t, h, "GET", "/api/dumps/graph?engine=mongodb", nil, cookie)
	json.NewDecoder(rec.Body).Decode(&g)
	if rec.Code != 200 || len(g.Nodes) != 0 || len(g.Lanes) != 0 {
		t.Fatalf("mongodb graph = %d %+v", rec.Code, g)
	}
	rec = doJSON(t, h, "GET", "/api/dumps/graph?engine=mysql", nil, cookie)
	if rec.Code != 400 {
		t.Fatalf("bad engine = %d", rec.Code)
	}
}
