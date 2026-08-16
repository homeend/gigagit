package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/promptstate"
)

// newNarrowClone builds the real narrowed-refspec situation: a bare origin, a
// SINGLE-BRANCH clone of it (so remote.origin.fetch maps only main), and a
// second branch pushed with -u from that clone. The push succeeds and git
// records branch.feat.remote/merge, but the refspec cannot map feat — so
// refs/remotes/origin/feat never materializes and every ↓↑ marker for feat
// stays blind. Returns (clone, origin).
func newNarrowClone(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	origin := filepath.Join(root, "origin.git")
	clone := filepath.Join(root, "clone")

	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seed, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seed, "add", "-A")
	gitRun(t, seed, "commit", "-m", "c1")
	gitRun(t, root, "clone", "--bare", seed, origin)

	gitRun(t, root, "clone", "--single-branch", "--branch", "main", origin, clone)
	gitRun(t, clone, "checkout", "-b", "feat")
	if err := os.WriteFile(filepath.Join(clone, "f.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, clone, "add", "-A")
	gitRun(t, clone, "commit", "-m", "c2")
	gitRun(t, clone, "push", "-u", "origin", "feat")
	return clone, origin
}

// stateServer wires a Server whose promptstate/MRU files live in stateDir, the
// health_test.go recipe. XDG_CONFIG_HOME is redirected too so the machine's own
// gg config never leaks into a test.
func stateServer(t *testing.T, repo, stateDir string) *Server {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := New(domain.Open(repo))
	srv.reposPath = filepath.Join(stateDir, "repos.toml")
	return srv
}

// A clone whose fetch refspec covers only one branch reports the branch it
// cannot track, with both a batch repair and a per-branch one.
func TestNotificationsReportsUnmappedBranch(t *testing.T) {
	clone, _ := newNarrowClone(t)
	ts := serve(t, stateServer(t, clone, t.TempDir()))

	var got noticesResp
	if code := getJSON(t, ts, "/api/notifications", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(got.Notices) != 1 {
		t.Fatalf("notices = %+v, want exactly one", got.Notices)
	}
	n := got.Notices[0]
	if n.ID != noticeNarrowRefspec {
		t.Errorf("id = %q, want %q", n.ID, noticeNarrowRefspec)
	}
	if len(n.Items) != 1 || n.Items[0] != "feat" {
		t.Errorf("items = %v, want [feat]", n.Items)
	}
	var batch, perBranch, silence bool
	for _, a := range n.Actions {
		switch {
		case a.Dismiss == noticePushMapOffer:
			silence = true // the escape hatch for the per-push fork
		case a.Op != opAddFetchMappings:
			t.Errorf("action op = %q", a.Op)
		case a.Branch == "":
			batch = true
		case a.Branch == "feat":
			perBranch = true
		}
	}
	if !batch || !perBranch || !silence {
		t.Errorf("actions = %+v, want a batch repair, a per-branch repair and the silence offer", n.Actions)
	}
}

// A healthy clone has nothing to say.
func TestNotificationsEmptyOnHealthyRepo(t *testing.T) {
	ts := serve(t, stateServer(t, newRepoDir(t, 2), t.TempDir()))
	var got noticesResp
	if code := getJSON(t, ts, "/api/notifications", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(got.Notices) != 0 {
		t.Errorf("notices = %+v, want none", got.Notices)
	}
}

// The repair writes the per-branch refspec, fetches it, and the tracking ref
// materializes — after which the finding is gone.
func TestAddFetchMappingsRepairsTracking(t *testing.T) {
	clone, _ := newNarrowClone(t)
	ts := serve(t, stateServer(t, clone, t.TempDir()))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"add-fetch-mappings"}`), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	specs := gitRun(t, clone, "config", "--get-all", "remote.origin.fetch")
	if want := "+refs/heads/feat:refs/remotes/origin/feat"; !hasLine(specs, want) {
		t.Errorf("refspecs = %q, want one to be %q", specs, want)
	}
	// The point of the whole feature: the remote-tracking ref now exists.
	gitRun(t, clone, "rev-parse", "--verify", "refs/remotes/origin/feat")

	var got noticesResp
	getJSON(t, ts, "/api/notifications", &got)
	if len(got.Notices) != 0 {
		t.Errorf("notices after repair = %+v, want none", got.Notices)
	}
}

// Only one branch is repaired when one is named.
func TestAddFetchMappingsSingleBranch(t *testing.T) {
	clone, _ := newNarrowClone(t)
	ts := serve(t, stateServer(t, clone, t.TempDir()))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"add-fetch-mappings","branch":"feat"}`), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	gitRun(t, clone, "rev-parse", "--verify", "refs/remotes/origin/feat")
}

// A branch gg did not just report as affected is refused: the name reaches a
// config write and a fetch argv, so membership is checked server-side.
func TestAddFetchMappingsRefusesUnaffectedBranch(t *testing.T) {
	clone, _ := newNarrowClone(t)
	ts := serve(t, stateServer(t, clone, t.TempDir()))

	if code := postJSON(t, ts, "/api/op", `{"op":"add-fetch-mappings","branch":"main"}`,
		"application/json", "", nil); code != http.StatusUnprocessableEntity {
		t.Errorf("code = %d, want 422", code)
	}
}

// Nothing to repair is a refusal, not a silent no-op run.
func TestAddFetchMappingsRefusesHealthyRepo(t *testing.T) {
	ts := serve(t, stateServer(t, newRepoDir(t, 1), t.TempDir()))
	if code := postJSON(t, ts, "/api/op", `{"op":"add-fetch-mappings"}`,
		"application/json", "", nil); code != http.StatusUnprocessableEntity {
		t.Errorf("code = %d, want 422", code)
	}
}

// THE storage test: a dismissal must survive a new Server, because `gg web`
// binds a random port and the browser's own storage starts empty every run.
// Only the state dir carries it across.
func TestNotificationDismissSurvivesANewServer(t *testing.T) {
	clone, _ := newNarrowClone(t)
	stateDir := t.TempDir()
	ts := serve(t, stateServer(t, clone, stateDir))

	if code := postJSON(t, ts, "/api/notifications/dismiss",
		`{"id":"narrow_fetch_refspec"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("dismiss status = %d, want 200", code)
	}
	// It landed under the git-common-dir key the TUI uses, so dismissing it
	// here silences the TUI's identical notice too.
	key, err := domain.Open(clone).GitCommonDir(context.Background())
	if err != nil || key == "" {
		t.Fatalf("GitCommonDir: %v %q", err, key)
	}
	store := promptstate.NewFileStore(filepath.Join(stateDir, "prompts.toml"))
	if !store.DismissedNotices(key)[noticeNarrowRefspec] {
		t.Fatalf("not persisted: %v", store.DismissedNotices(key))
	}

	// A second Server on the same state dir — the restart, minus the port.
	ts2 := serve(t, stateServer(t, clone, stateDir))
	var got noticesResp
	if code := getJSON(t, ts2, "/api/notifications", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(got.Notices) != 0 {
		t.Errorf("notices after restart = %+v, want none", got.Notices)
	}
	if !got.Suppressed[noticeNarrowRefspec] {
		t.Errorf("suppressed = %v, want the dismissed id", got.Suppressed)
	}
}

// The post-push offer has its own id, and suppressing it does NOT hide the
// finding from the centre — the offer stops nagging, the row stays.
func TestPushOfferSuppressionKeepsTheNotice(t *testing.T) {
	clone, _ := newNarrowClone(t)
	ts := serve(t, stateServer(t, clone, t.TempDir()))

	if code := postJSON(t, ts, "/api/notifications/dismiss",
		`{"id":"web_push_mapping_offer"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("dismiss status = %d, want 200", code)
	}
	var got noticesResp
	getJSON(t, ts, "/api/notifications", &got)
	if !got.Suppressed[noticePushMapOffer] {
		t.Errorf("suppressed = %v, want the offer id", got.Suppressed)
	}
	if len(got.Notices) != 1 {
		t.Fatalf("notices = %+v, want the finding to remain", got.Notices)
	}
	for _, a := range got.Notices[0].Actions {
		if a.Dismiss == noticePushMapOffer {
			t.Errorf("the silence offer is still on show after being taken: %+v", got.Notices[0].Actions)
		}
	}
}

// An id nothing ships is refused: prompts.toml is never garbage-collected, so
// a client bug must not be able to write junk keys into it.
func TestNotificationDismissRejectsUnknownID(t *testing.T) {
	ts := serve(t, stateServer(t, newRepoDir(t, 1), t.TempDir()))
	if code := postJSON(t, ts, "/api/notifications/dismiss",
		`{"id":"not_a_notice"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", code)
	}
}

// hasLine reports whether any line of out equals want.
func hasLine(out, want string) bool {
	return slices.Contains(strings.Split(out, "\n"), want)
}

// engine.Push's post-push "add a tracking mapping?" fork fires on every push of
// an unmapped branch. Once the user turns it off for this repo it must be
// answered "skip" without a modal — a suppressed prompt nobody can answer would
// otherwise park the operation for five minutes.
func TestSuppressedFetchMappingForkIsAnsweredSkip(t *testing.T) {
	clone, _ := newNarrowClone(t)
	stateDir := t.TempDir()
	srv := stateServer(t, clone, stateDir)
	ts := serve(t, srv)

	req := engine.PromptReq(engine.FetchMappingDecisionID, "add a mapping?", []string{"add", "skip"})
	if _, ok := autoAnswer(srv, req); ok {
		t.Fatal("claimed the fork before anything was suppressed")
	}
	if code := postJSON(t, ts, "/api/notifications/dismiss",
		`{"id":"web_push_mapping_offer"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("dismiss status = %d, want 200", code)
	}
	opt, ok := autoAnswer(srv, req)
	if !ok || opt != "skip" {
		t.Errorf("autoAnswer = %q/%v, want skip/true", opt, ok)
	}
	// Only THIS fork is claimed — a policy that swallowed other decisions would
	// answer questions nobody asked it about.
	other := engine.PromptReq("merge.strategy", "how?", []string{"merge", "abort"})
	if _, ok := autoAnswer(srv, other); ok {
		t.Error("claimed an unrelated decision")
	}
}

// A suppressed fork must be answered by the SERVER, from inside the operation
// that raised it — and answering it must not need git. The repo key comes from
// a cache for exactly that reason: svc.GitCommonDir takes a Read reservation on
// the repo gate, the pushing operation already holds a write one, and asking
// there deadlocks until the decide timeout. This test hangs (and then fails on
// the timeout) if that regresses.
func TestSuppressedForkDoesNotWedgeThePush(t *testing.T) {
	clone, origin := newNarrowClone(t)
	srv := stateServer(t, clone, t.TempDir())
	srv.decideTimeout = 3 * time.Second // an unanswered fork must not stall the test
	ts := serve(t, srv)

	// The centre is what teaches the server its repo key, exactly as the client
	// does at boot.
	var seen noticesResp
	getJSON(t, ts, "/api/notifications", &seen)
	if code := postJSON(t, ts, "/api/notifications/dismiss",
		`{"id":"web_push_mapping_offer"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("dismiss status = %d, want 200", code)
	}

	gitRun(t, clone, "commit", "--allow-empty", "-m", "another")
	start := time.Now()
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"push"}`), 60*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("push did not finish cleanly: %v", done)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("push took %s — the fork was not answered promptly", elapsed)
	}
	// "skip" was the answer, so no mapping was written.
	if hasLine(gitRun(t, clone, "config", "--get-all", "remote.origin.fetch"),
		"+refs/heads/feat:refs/remotes/origin/feat") {
		t.Error("the suppressed fork was answered add, not skip")
	}
	// And the push itself did happen.
	if gitRun(t, origin, "rev-parse", "refs/heads/feat") != gitRun(t, clone, "rev-parse", "refs/heads/feat") {
		t.Error("origin/feat did not receive the push")
	}
}
