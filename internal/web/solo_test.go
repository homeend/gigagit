package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// scopeAnswer is /api/commits' view of the scope: the whole list, plus the
// one-entry solo fields.
type scopeAnswer struct {
	Solo     string `json:"solo"`
	SoloKind string `json:"solo_kind"`
	Scope    []struct {
		Kind string `json:"kind"`
		Ref  string `json:"ref"`
	} `json:"scope"`
	Rows []struct {
		Subject string `json:"subject"`
	} `json:"rows"`
}

func readScope(t *testing.T, ts *httptest.Server) scopeAnswer {
	t.Helper()
	var out scopeAnswer
	if code := getJSON(t, ts, "/api/commits", &out); code != http.StatusOK {
		t.Fatalf("commits code = %d", code)
	}
	return out
}

func (a scopeAnswer) refs() []string {
	var out []string
	for _, e := range a.Scope {
		out = append(out, e.Ref)
	}
	return out
}

func (a scopeAnswer) subjects() []string {
	var out []string
	for _, r := range a.Rows {
		out = append(out, r.Subject)
	}
	return out
}

// The TUI's Add / Remove from commit view: one ordered list, solo being a
// list of one. Two members answer no single solo; removing back to one
// answers it again; removing the last returns to every branch.
func TestSoloAddRemoveBuildsSet(t *testing.T) {
	t.Parallel()
	dir := soloRepo(t)
	gitRun(t, dir, "checkout", "-b", "other")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "only on other")
	gitRun(t, dir, "checkout", "main")
	ts := serve(t, New(domain.Open(dir)))

	for _, b := range []string{"feature", "main"} {
		if code := setSoloHTTP(t, ts, `{"op":"add","ref":"`+b+`"}`); code != http.StatusOK {
			t.Fatalf("add %s code = %d", b, code)
		}
	}
	a := readScope(t, ts)
	if got := a.refs(); len(got) != 2 || got[0] != "feature" || got[1] != "main" {
		t.Fatalf("scope = %v, want [feature main]", got)
	}
	if a.Solo != "" {
		t.Errorf("solo = %q with two members, want empty", a.Solo)
	}
	if subs := a.subjects(); !has(subs, "only on feature") || has(subs, "only on other") {
		t.Errorf("two-branch feed = %v, want feature's history and not other's", subs)
	}
	// Adding a member again changes nothing.
	if code := setSoloHTTP(t, ts, `{"op":"add","ref":"main"}`); code != http.StatusOK {
		t.Fatalf("re-add code = %d", code)
	}
	if got := readScope(t, ts).refs(); len(got) != 2 {
		t.Errorf("re-add scope = %v, want two entries", got)
	}

	if code := setSoloHTTP(t, ts, `{"op":"remove","kind":"branch","ref":"feature"}`); code != http.StatusOK {
		t.Fatalf("remove code = %d", code)
	}
	a = readScope(t, ts)
	if a.Solo != "main" || a.SoloKind != "branch" {
		t.Errorf("one member left: solo = %q/%q, want main/branch", a.Solo, a.SoloKind)
	}
	if code := setSoloHTTP(t, ts, `{"op":"remove","kind":"branch","ref":"main"}`); code != http.StatusOK {
		t.Fatalf("remove last code = %d", code)
	}
	a = readScope(t, ts)
	if len(a.Scope) != 0 || a.Solo != "" {
		t.Errorf("after removing the last: scope = %v solo = %q, want every branch", a.refs(), a.Solo)
	}
	if !has(a.subjects(), "only on other") {
		t.Errorf("feed did not widen back: %v", a.subjects())
	}
}

// Remove asks only for membership: a member branch deleted since it was
// added must still leave the list, or the scope could never be emptied
// except by Show all branches.
func TestSoloRemoveDeletedBranch(t *testing.T) {
	t.Parallel()
	dir := soloRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	for _, b := range []string{"feature", "main"} {
		if code := setSoloHTTP(t, ts, `{"op":"add","ref":"`+b+`"}`); code != http.StatusOK {
			t.Fatalf("add %s code = %d", b, code)
		}
	}
	gitRun(t, dir, "branch", "-D", "feature")
	if code := setSoloHTTP(t, ts, `{"op":"remove","kind":"branch","ref":"feature"}`); code != http.StatusOK {
		t.Fatalf("remove deleted code = %d", code)
	}
	if got := readScope(t, ts).refs(); len(got) != 1 || got[0] != "main" {
		t.Errorf("scope = %v, want [main]", got)
	}
}

// Add is a Branches row in the TUI: a local branch, never a tag (a tag has
// its own solo), never a name that does not exist, never an option.
func TestSoloAddRefusesTagAndUnknown(t *testing.T) {
	t.Parallel()
	dir := soloRepo(t)
	gitRun(t, dir, "tag", "v1")
	ts := serve(t, New(domain.Open(dir)))
	for body, want := range map[string]int{
		`{"op":"add","ref":"v1"}`:      http.StatusNotFound,
		`{"op":"add","ref":"nope"}`:    http.StatusNotFound,
		`{"op":"add","ref":"--all"}`:   http.StatusBadRequest,
		`{"op":"add","ref":""}`:        http.StatusBadRequest,
		`{"op":"twist","ref":"main"}`:  http.StatusBadRequest,
		`{"op":"remove","ref":"main"}`: http.StatusOK, // not a member: nothing to do
	} {
		if code := setSoloHTTP(t, ts, body); code != want {
			t.Errorf("%s code = %d, want %d", body, code, want)
		}
	}
	if got := readScope(t, ts).refs(); len(got) != 0 {
		t.Errorf("refused requests changed the scope: %v", got)
	}
}

// Solo SETS the list: after building a set, soloing one branch leaves that
// branch alone, the TUI's commitSoloRow.
func TestSoloSetReplacesSet(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(soloRepo(t))))
	for _, b := range []string{"feature", "main"} {
		if code := setSoloHTTP(t, ts, `{"op":"add","ref":"`+b+`"}`); code != http.StatusOK {
			t.Fatalf("add %s code = %d", b, code)
		}
	}
	if code := setSoloHTTP(t, ts, `{"branch":"feature"}`); code != http.StatusOK {
		t.Fatalf("solo code = %d", code)
	}
	if got := readScope(t, ts).refs(); len(got) != 1 || got[0] != "feature" {
		t.Errorf("scope = %v, want [feature]", got)
	}
}

// A sha left by Solo from this commit stays in the list when a branch is
// added (the TUI appends to commitScopeBranches whatever it holds).
func TestSoloAddKeepsCommitEntry(t *testing.T) {
	t.Parallel()
	dir := soloRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	sha := strings.TrimSpace(gitOut(t, dir, "rev-parse", "main"))
	if code := setSoloHTTP(t, ts, `{"kind":"commit","ref":"`+sha+`"}`); code != http.StatusOK {
		t.Fatalf("commit solo code = %d", code)
	}
	if code := setSoloHTTP(t, ts, `{"op":"add","ref":"feature"}`); code != http.StatusOK {
		t.Fatalf("add code = %d", code)
	}
	a := readScope(t, ts)
	if len(a.Scope) != 2 || a.Scope[0].Kind != "commit" || a.Scope[0].Ref != sha || a.Scope[1].Ref != "feature" {
		t.Errorf("scope = %+v, want [commit %s, branch feature]", a.Scope, sha)
	}
	// The commit entry is removed by kind + ref.
	if code := setSoloHTTP(t, ts, `{"op":"remove","kind":"commit","ref":"`+sha+`"}`); code != http.StatusOK {
		t.Fatalf("remove commit code = %d", code)
	}
	if got := readScope(t, ts).refs(); len(got) != 1 || got[0] != "feature" {
		t.Errorf("scope = %v, want [feature]", got)
	}
}
