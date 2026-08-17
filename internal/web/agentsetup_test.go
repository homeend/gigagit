package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentskill"
	"github.com/homeend/gigagit/internal/domain"
)

// agentRepo builds a repo with an isolated HOME. Agent detection probes the
// home directory and installing writes into it, so a test that skipped this
// would read — and then write — the developer's own ~/.claude.
func agentRepo(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return newRepoDir(t, 1)
}

type agentsResp struct {
	Version int        `json:"version"`
	Project string     `json:"project"`
	Agents  []agentRow `json:"agents"`
}

func agentList(t *testing.T, ts *httptest.Server) agentsResp {
	t.Helper()
	var out agentsResp
	if code := getJSON(t, ts, "/api/agents", &out); code != http.StatusOK {
		t.Fatalf("GET /api/agents = %d", code)
	}
	return out
}

func findAgent(rows []agentRow, id string) *agentRow {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

// Only agents that are actually PRESENT are listed: a project with no agent
// directories offers nothing to install.
func TestAgentsListsOnlyDetected(t *testing.T) {
	dir := agentRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	body := agentList(t, ts)
	if body.Version != agentskill.Version {
		t.Errorf("version = %d, want the embedded skill's %d", body.Version, agentskill.Version)
	}
	if findAgent(body.Agents, "claude-project") != nil {
		t.Error("claude-project detected with no .claude directory")
	}

	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	row := findAgent(agentList(t, ts).Agents, "claude-project")
	if row == nil {
		t.Fatal("claude-project missing after .claude appeared")
	}
	if row.Status != "new" || row.Checked {
		t.Errorf("fresh target = %+v, want status new and unchecked (a first install is opt-in)", *row)
	}
	if !strings.HasSuffix(filepath.ToSlash(row.Target), ".claude/skills/using-gg/SKILL.md") {
		t.Errorf("target = %q", row.Target)
	}
}

func TestAgentInstallWritesSkillAndReportsStatus(t *testing.T) {
	dir := agentRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	var out map[string]any
	code := postJSON(t, ts, "/api/agents/install", `{"id":"claude-project"}`, "application/json", "", &out)
	if code != http.StatusOK {
		t.Fatalf("install = %d (%v)", code, out)
	}
	if out["was"] != "new" || out["status"] != "up to date" {
		t.Errorf("install reported %v", out)
	}
	target := filepath.Join(dir, ".claude", "skills", "using-gg", "SKILL.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("skill not written: %v", err)
	}
	if !agentskill.HasMarker(data) {
		t.Error("written file carries no gg marker")
	}
	// The listing must reflect the install — this is the status a second visit
	// shows, and the reason the row defaults to checked from now on.
	row := findAgent(agentList(t, ts).Agents, "claude-project")
	if row == nil || row.Status != "up to date" || !row.Checked {
		t.Errorf("row after install = %+v", row)
	}
}

// The wire names an agent; it never names a path. An id nothing detected is a
// 404, and nothing is written.
func TestAgentInstallRefusesUndetectedID(t *testing.T) {
	dir := agentRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"id":"claude-project"}`, // real id, but nothing detected it here
		`{"id":"../../etc/profile"}`,
		`{"id":""}`,
	} {
		var out map[string]any
		if code := postJSON(t, ts, "/api/agents/install", body, "application/json", "", &out); code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", body, code)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); err == nil {
		t.Fatal("a refused install created the target directory anyway")
	}
}

// Installing again over gg's own file is idempotent — that is what "refresh an
// outdated skill" does, and it must not append or duplicate.
func TestAgentInstallIsIdempotent(t *testing.T) {
	dir := agentRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))
	target := filepath.Join(dir, ".claude", "skills", "using-gg", "SKILL.md")

	postJSON(t, ts, "/api/agents/install", `{"id":"claude-project"}`, "application/json", "", nil)
	first, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	postJSON(t, ts, "/api/agents/install", `{"id":"claude-project"}`, "application/json", "", nil)
	second, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("a second install changed the file")
	}
}
