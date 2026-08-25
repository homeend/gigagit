package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// soloRepo: main with one commit, feature with a second commit only it has.
// Solo on either branch is then observable by which subjects come back.
func soloRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main", ".")
	gitRun(t, dir, "config", "user.email", "t@example.com")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "on main")
	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "only on feature")
	gitRun(t, dir, "checkout", "main")
	return dir
}

func commitSubjects(t *testing.T, ts *httptest.Server) ([]string, string) {
	t.Helper()
	var out struct {
		Rows []struct {
			Subject string `json:"subject"`
		} `json:"rows"`
		Solo string `json:"solo"`
	}
	if code := getJSON(t, ts, "/api/commits", &out); code != http.StatusOK {
		t.Fatalf("commits code = %d", code)
	}
	subs := make([]string, len(out.Rows))
	for i, r := range out.Rows {
		subs[i] = r.Subject
	}
	return subs, out.Solo
}

func setSoloHTTP(t *testing.T, ts *httptest.Server, body string) int {
	t.Helper()
	var out map[string]any
	return postJSON(t, ts, "/api/solo", body, "application/json", "", &out)
}

func has(subs []string, want string) bool {
	for _, s := range subs {
		if s == want {
			return true
		}
	}
	return false
}

func TestSoloNarrowsAndRestores(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(soloRepo(t))))

	subs, solo := commitSubjects(t, ts)
	if solo != "" {
		t.Errorf("initial solo = %q, want empty", solo)
	}
	if !has(subs, "only on feature") {
		t.Fatalf("unscoped feed missing the feature commit: %v", subs)
	}

	if code := setSoloHTTP(t, ts, `{"branch":"main"}`); code != http.StatusOK {
		t.Fatalf("solo main code = %d", code)
	}
	subs, solo = commitSubjects(t, ts)
	if solo != "main" {
		t.Errorf("solo = %q, want main", solo)
	}
	if has(subs, "only on feature") {
		t.Errorf("solo main still shows feature-only history: %v", subs)
	}
	if !has(subs, "on main") {
		t.Errorf("solo main lost main's own history: %v", subs)
	}

	// Clearing restores the whole repo.
	if code := setSoloHTTP(t, ts, `{"branch":""}`); code != http.StatusOK {
		t.Fatalf("clear code = %d", code)
	}
	subs, solo = commitSubjects(t, ts)
	if solo != "" {
		t.Errorf("solo = %q after clear, want empty", solo)
	}
	if !has(subs, "only on feature") {
		t.Errorf("clearing solo did not restore the full feed: %v", subs)
	}
}

// The trap this feature was most likely to fall into: an op that changes
// state drops the cached feed (resetFeed), and a scope applied once to the
// old feed object would silently vanish with it — you would be kicked out of
// solo by an unrelated commit, with the chip still claiming you are in it.
func TestSoloSurvivesFeedResetAfterOp(t *testing.T) {
	t.Parallel()
	dir := soloRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	if code := setSoloHTTP(t, ts, `{"branch":"main"}`); code != http.StatusOK {
		t.Fatalf("solo code = %d", code)
	}
	if _, solo := commitSubjects(t, ts); solo != "main" {
		t.Fatalf("solo = %q before the op", solo)
	}

	// Any op reporting changed:true resets the feed. A commit is the cheapest.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"commit","message":"new work"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true || done["changed"] != true {
		t.Fatalf("commit done = %v", done)
	}

	subs, solo := commitSubjects(t, ts)
	if solo != "main" {
		t.Errorf("solo = %q after the op, want main — the scope was dropped with the feed", solo)
	}
	if has(subs, "only on feature") {
		t.Errorf("feed widened back to every branch after the op: %v", subs)
	}
	if !has(subs, "new work") {
		t.Errorf("the new commit is missing from the soloed feed: %v", subs)
	}
}

// A scope that cannot render is worse than no scope: every later /api/commits
// would fail, taking the client's exit affordance with it. So an unknown
// branch is refused at the door and the previous scope is left untouched.
func TestSoloUnknownBranchRefusedAndScopeUnchanged(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(soloRepo(t))))

	if code := setSoloHTTP(t, ts, `{"branch":"main"}`); code != http.StatusOK {
		t.Fatalf("solo main code = %d", code)
	}
	if code := setSoloHTTP(t, ts, `{"branch":"no-such-branch"}`); code != http.StatusNotFound {
		t.Errorf("unknown branch code = %d, want 404", code)
	}
	subs, solo := commitSubjects(t, ts)
	if solo != "main" {
		t.Errorf("solo = %q, want main — a refused request changed the scope", solo)
	}
	if has(subs, "only on feature") {
		t.Errorf("scope was lost: %v", subs)
	}
}

func TestSoloRejectsOptionLikeBranch(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(soloRepo(t))))
	if code := setSoloHTTP(t, ts, `{"branch":"--all"}`); code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", code)
	}
}

// Solo selects refs; it is not a content filter, so the lane graph must still
// be laid out (the TUI suppresses its graph only for path/author/grep filters).
func TestSoloKeepsGraphCells(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(soloRepo(t))))
	if code := setSoloHTTP(t, ts, `{"branch":"main"}`); code != http.StatusOK {
		t.Fatalf("solo code = %d", code)
	}
	var out struct {
		Rows []struct {
			Cells string `json:"cells"`
		} `json:"rows"`
	}
	if code := getJSON(t, ts, "/api/commits", &out); code != http.StatusOK {
		t.Fatalf("commits code = %d", code)
	}
	if len(out.Rows) == 0 {
		t.Fatal("no rows")
	}
	if out.Rows[0].Cells == "" {
		t.Error("graph cells are empty under solo — ref selection must not suppress the graph")
	}
}
