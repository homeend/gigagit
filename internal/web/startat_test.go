package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

func openRepo(t *testing.T, dir string) *domain.Service {
	t.Helper()
	return domain.Open(dir)
}

// startAtGet reads the page's start-at hand-out: the body is always JSON
// (core.js's getJSON decodes every answer), {} when nothing is pending.
func startAtGet(t *testing.T, s *Server) (int, startAtBody) {
	t.Helper()
	ts := serve(t, s)
	var body startAtBody
	code := getJSON(t, ts, "/api/session/start-at", &body)
	return code, body
}

// `gg open --web` hands the command to the page ONCE: the first tab to boot
// lands on it, a reload or a second tab does not jump again (the TUI's --at
// fires once too).
func TestStartAtIsHandedOutOnce(t *testing.T) {
	t.Parallel()
	s := New(openRepo(t, newRepoDir(t, 1)))
	if err := s.setStartAt(steer.Command{
		Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 12},
	}); err != nil {
		t.Fatal(err)
	}
	code, body := startAtGet(t, s)
	if code != http.StatusOK || body.Steer == nil {
		t.Fatalf("first GET = %d %+v, want 200 with a steer", code, body)
	}
	if body.Steer.Cmd != "navigate" || body.Steer.File != "a.txt" || body.Steer.Line != 12 || body.Steer.Side != "new" || body.Steer.State != "unstaged" {
		t.Errorf("steer = %+v", *body.Steer)
	}
	code, body = startAtGet(t, s)
	if code != http.StatusOK || body.Steer != nil {
		t.Errorf("second GET = %d %+v, want 200 with no steer", code, body)
	}
}

// No start-at at all (plain `gg web`) is the same empty answer, not an error.
func TestStartAtIsEmptyByDefault(t *testing.T) {
	t.Parallel()
	code, body := startAtGet(t, New(openRepo(t, newRepoDir(t, 1))))
	if code != http.StatusOK || body.Steer != nil {
		t.Errorf("GET = %d %+v, want 200 {}", code, body)
	}
}

// The command is validated at boot through the same allowlists a posted steer
// goes through, so a bad link cannot reach the page — Serve refuses to start.
func TestStartAtRefusesABadCommand(t *testing.T) {
	t.Parallel()
	s := New(openRepo(t, newRepoDir(t, 1)))
	if err := s.setStartAt(steer.Command{Cmd: "teleport"}); err == nil {
		t.Fatal("setStartAt accepted an unknown command")
	}
	if _, body := startAtGet(t, s); body.Steer != nil {
		t.Errorf("a refused command was still handed out: %+v", *body.Steer)
	}
}

// A preview start-at carries the PAIR and no commit, exactly as a posted
// preview navigate does — the page resolves the tip itself.
func TestStartAtCarriesAPreviewPair(t *testing.T) {
	t.Parallel()
	s := New(openRepo(t, newRepoDir(t, 1)))
	if err := s.setStartAt(steer.Command{
		Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1},
	}); err != nil {
		t.Fatal(err)
	}
	_, body := startAtGet(t, s)
	if body.Steer == nil || body.Steer.State != "preview" || body.Steer.Source != "feat/x" || body.Steer.Target != "main" || body.Steer.Commit != "" {
		t.Fatalf("steer = %+v", body.Steer)
	}
}

// The page applies the start-at only after its FIRST FULL LOAD — status,
// branches and previews included, the fetches boot() otherwise lets run in
// the background — because a working-tree landing needs statusEntries and a
// preview landing needs the previews list (the TUI's startAtReady has the
// same previews-seen clause).
func TestStartAtPageWiring(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"live.js", `"/api/session/start-at"`, "live.js fetches the start-at hand-out"},
		{"live.js", "export { applyStartAt,", "applyStartAt must be exported for boot()"},
		{"app.js", "applyStartAt", "boot() must apply the start-at"},
		{"app.js", "Promise.allSettled", "the start-at waits for the first full load, not just commits"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// Ordering inside boot(): connectLive() first (the hub must be up before
	// the landing so a steer posted right after cannot be lost), then the
	// gate, then the start-at.
	app := read("app.js")
	i, j, k := strings.Index(app, "connectLive();"), strings.Index(app, "Promise.allSettled"), strings.Index(app, "applyStartAt(")
	if !(i > 0 && i < j && j < k) {
		t.Errorf("boot() order: connectLive at %d, allSettled at %d, applyStartAt at %d — want connectLive < allSettled < applyStartAt", i, j, k)
	}
}
