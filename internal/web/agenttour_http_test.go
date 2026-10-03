package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// A spawned worker's brief and its reports open as tours: the page asks
// the server to file one and learns which overview and which worktree.
func TestAgentTourEndpoint(t *testing.T) {
	testSessionManager(t)
	dir := newRepoDir(t, 1)
	tc := config.ToolCommand{Category: "session", Name: "Sh", Mode: "session", Command: "sh -c 'sleep 600'"}
	s, _, err := domain.Open(dir).StartAgentSession(context.Background(), tc, dir, "", 80, 24, nil, "",
		domain.SpawnRecord{Parent: "x/ov", Brief: "# B\n[a](a.go:1)", Worktree: dir, Spawned: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(domain.UseSessionStates(domain.NewStaticStates(nil)))
	id := string(s.Info().ID)
	ts := serve(t, New(domain.Open(dir)))
	var list struct {
		Sessions []map[string]any `json:"sessions"`
	}
	getJSON(t, ts, "/api/sessions", &list)
	if len(list.Sessions) != 1 || list.Sessions[0]["has_brief"] != true || list.Sessions[0]["has_report"] != nil {
		t.Fatalf("wire = %+v", list.Sessions)
	}
	post := func(kind string) (int, map[string]string) {
		var out map[string]string
		code := postJSON(t, ts, "/api/agent-tour", `{"id":"`+id+`","kind":"`+kind+`"}`, "application/json", "", &out)
		return code, out
	}
	code, out := post("brief")
	if code != http.StatusOK || out["overview"] == "" || out["worktree"] != dir {
		t.Fatalf("brief = %d %+v", code, out)
	}
	// The page focuses it right away: it must already be on the open-file list.
	if code := postJSON(t, ts, "/api/open-files", `{"op":"focus","id":"`+out["overview"]+`","tab":"t1"}`, "application/json", "", &map[string]any{}); code != http.StatusOK {
		t.Fatalf("focus right after the tour answer = %d", code)
	}
	if code, again := post("brief"); code != http.StatusOK || again["overview"] != out["overview"] {
		t.Fatalf("a second open reuses the tour: %d %+v vs %+v", code, again, out)
	}
	if code, _ := post("report"); code != http.StatusConflict {
		t.Fatalf("report before any = %d", code)
	}
	if _, err := domain.AgentReportVerb(domain.FullSessionID(s.Info().ID), "done", true); err != nil {
		t.Fatal(err)
	}
	if code, out := post("report"); code != http.StatusOK || out["overview"] == "" {
		t.Fatalf("report = %d %+v", code, out)
	}
	if code, _ := post("notes"); code != http.StatusBadRequest {
		t.Fatalf("bad kind = %d", code)
	}
	var e map[string]string
	if code := postJSON(t, ts, "/api/agent-tour", `{"id":"s99","kind":"brief"}`, "application/json", "", &e); code != http.StatusNotFound {
		t.Fatalf("unknown session = %d", code)
	}
	getJSON(t, ts, "/api/sessions", &list)
	if list.Sessions[0]["has_report"] != true || !strings.HasPrefix(out["overview"], "f") {
		t.Fatalf("wire after a report = %+v", list.Sessions)
	}
}
