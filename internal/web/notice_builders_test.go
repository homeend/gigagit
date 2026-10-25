package web

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	pf "github.com/homeend/gigagit/internal/preflight"
	"github.com/homeend/gigagit/internal/promptstate"
)

func actionLabels(as []noticeAction) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.Label)
	}
	return out
}

// The stale-lock notice is the TUI's: a count title, every lock with its
// age, and one removal of exactly the locks it lists.
func TestStaleLockNotice(t *testing.T) {
	t.Parallel()
	now := time.Now()
	ctx := context.Background()
	if got := staleLockNotice(ctx, nil, nil, model.RepoHealth{}, nil); got != nil {
		t.Fatalf("no locks: %+v", got)
	}
	two := model.RepoHealth{StaleLocks: []model.GitLock{
		{Path: "/r/.git/index.lock", Name: "index.lock", ModTime: now.Add(-5 * time.Minute)},
		{Path: "/r/.git/HEAD.lock", Name: "HEAD.lock", ModTime: now.Add(-3 * time.Hour)},
	}}
	n := staleLockNotice(ctx, nil, nil, two, nil)
	if len(n) != 1 || n[0].ID != noticeStaleLock || n[0].Title != "2 git lock files are present — operations will fail" {
		t.Fatalf("two locks: %+v", n)
	}
	detail := strings.Join(n[0].Detail, "\n")
	for _, want := range []string{"index.lock  (5m old)", "HEAD.lock  (3h old)", "Only remove these if no other git is running right now"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail lacks %q:\n%s", want, detail)
		}
	}
	rm := n[0].Actions[0]
	if rm.Op != "remove-locks" || rm.Label != "Remove the lock files" || strings.Join(rm.Paths, ",") != "/r/.git/index.lock,/r/.git/HEAD.lock" {
		t.Errorf("removal = %+v", rm)
	}
	if got := actionLabels(n[0].Actions[1:]); strings.Join(got, "|") != labelNotNow+"|"+labelNever {
		t.Errorf("dismissal rows = %v", got)
	}
	one := model.RepoHealth{StaleLocks: two.StaleLocks[:1]}
	n = staleLockNotice(ctx, nil, nil, one, nil)
	if n[0].Title != "A git lock file is present — operations will fail" || n[0].Actions[0].Label != "Remove the lock file" {
		t.Errorf("one lock: %q / %q", n[0].Title, n[0].Actions[0].Label)
	}
}

// A lock younger than a few seconds is a git running right now (the page's
// own status scan right after a worktree switch, an agent's git): the badge
// counted it and the centre, opened a moment later, said "nothing to
// report". Only an older lock is a notice; the lock bar's second look
// (locks.js) still shows a stranded one at once.
func TestStaleLockNoticeSkipsALiveGitsLock(t *testing.T) {
	t.Parallel()
	now := time.Now()
	ctx := context.Background()
	live := model.RepoHealth{StaleLocks: []model.GitLock{{Path: "/r/.git/index.lock", Name: "index.lock", ModTime: now.Add(-time.Second)}}}
	if got := staleLockNotice(ctx, nil, nil, live, nil); got != nil {
		t.Errorf("a 1s-old lock is a live git's: %+v", got)
	}
	mixed := model.RepoHealth{StaleLocks: []model.GitLock{
		{Path: "/r/.git/index.lock", Name: "index.lock", ModTime: now.Add(-time.Second)},
		{Path: "/r/.git/HEAD.lock", Name: "HEAD.lock", ModTime: now.Add(-10 * time.Minute)},
	}}
	n := staleLockNotice(ctx, nil, nil, mixed, nil)
	if len(n) != 1 || n[0].Title != "A git lock file is present — operations will fail" || strings.Join(n[0].Actions[0].Paths, ",") != "/r/.git/HEAD.lock" {
		t.Errorf("only the old lock: %+v", n)
	}
}

// The commit-graph recommendation joins the centre on the TUI's condition,
// with its two fixes.
func TestCommitGraphNotice(t *testing.T) {
	t.Parallel()
	srv := New(domain.Open(newRepoDir(t, 1)))
	srv.packThreshold = 10
	ctx := context.Background()
	big := model.RepoHealth{PackBytes: 20}
	n := commitGraphNotice(ctx, srv, nil, big, nil)
	if len(n) != 1 || n[0].ID != noticeCommitGraph || n[0].Title != "Commit browsing can be ~10× faster in this repo" {
		t.Fatalf("big repo: %+v", n)
	}
	ops := []string{n[0].Actions[0].Op, n[0].Actions[1].Op}
	if ops[0] != "commit-graph" || ops[1] != opCommitGraphAuto {
		t.Errorf("fix ops = %v", ops)
	}
	for _, h := range []model.RepoHealth{{PackBytes: 5}, {PackBytes: 20, HasCommitGraph: true}, {PackBytes: 20, WriteCommitGraphSet: true}} {
		if got := commitGraphNotice(ctx, srv, nil, h, nil); got != nil {
			t.Errorf("%+v: %+v", h, got)
		}
	}
}

// "Enable auto-refresh only" sets fetch.writeCommitGraph and nothing else.
func TestCommitGraphAutoOp(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	runOpOK(t, ts, `{"op":"`+opCommitGraphAuto+`"}`)
	if got := gitRun(t, dir, "config", "--get", "fetch.writeCommitGraph"); strings.TrimSpace(got) != "true" {
		t.Errorf("fetch.writeCommitGraph = %q", got)
	}
}

// Every non-Satisfied, non-Silent verdict is a feature-disabled notice.
func TestFeatureDisabledNotices(t *testing.T) {
	t.Parallel()
	vs := []pf.Verdict{
		{Feature: pf.Feature{ID: "versions"}, State: pf.Repairable, Reason: pf.Text{Format: "old %s", Args: []any{"store"}}},
		{Feature: pf.Feature{ID: "note-edits"}, State: pf.Unsatisfiable, Reason: pf.Text{Format: "newer"}, Remedy: pf.Text{Format: "upgrade gg"}},
		{Feature: pf.Feature{ID: "forge", Silent: true}, State: pf.Unsatisfiable},
		{Feature: pf.Feature{ID: "fine"}, State: pf.Satisfied},
	}
	n := featureDisabledFor(vs)
	if len(n) != 2 {
		t.Fatalf("notices = %+v, want 2", n)
	}
	if n[0].ID != "feature_disabled_versions" || n[0].Title != "versions is unavailable in this repository" ||
		strings.Join(n[0].Detail, "|") != "old store|Run `gg migrate` to repair this, or answer the migration prompt at the next launch." {
		t.Errorf("repairable = %+v", n[0])
	}
	if n[1].ID != noticeNoteEdits || strings.Join(n[1].Detail, "|") != "newer|upgrade gg" {
		t.Errorf("unsatisfiable = %+v", n[1])
	}
	if got := actionLabels(n[0].Actions); strings.Join(got, "|") != labelNotNow+"|"+labelNever {
		t.Errorf("rows = %v", got)
	}
}

// Tool-template offers and unsupported agents point at External tools; the
// offer count has no Never (a new offer must surface again).
func TestToolNotices(t *testing.T) {
	t.Parallel()
	srv, _, upd := toolStatusServer(t)
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml") // a private prompt store
	uns := upd
	uns.Kind, uns.AgentVersion = domain.ToolUnsupported, "9.9"
	uns.Block.Name = "another block"
	srv.toolStatuses = func(context.Context) []domain.ToolTemplateStatus { return []domain.ToolTemplateStatus{upd, uns} }
	n := toolTemplateNotices(context.Background(), srv, nil, model.RepoHealth{}, nil)
	if len(n) != 2 {
		t.Fatalf("notices = %+v", n)
	}
	if n[0].ID != noticeToolTemplateUpdate || n[0].Title != "External-tool template updates available: 1" ||
		strings.Join(actionLabels(n[0].Actions), "|") != "Open external tools|"+labelNotNow || n[0].Actions[0].Open != "exttools" {
		t.Errorf("update notice = %+v", n[0])
	}
	wantID := noticeToolAgentUnsupportedPrefix + promptstate.ToolUpdateID(uns.Block.Key())[:12]
	if n[1].ID != wantID || strings.Join(actionLabels(n[1].Actions), "|") != "Open external tools|"+labelNotNow+"|"+labelNever {
		t.Errorf("unsupported notice = %+v (want id %s)", n[1], wantID)
	}
	// An offer answered "Keep mine" no longer counts.
	store := srv.promptStore()
	if err := store.DeclineToolUpdate(upd.OfferKey()); err != nil {
		t.Fatal(err)
	}
	if n := toolTemplateNotices(context.Background(), srv, nil, model.RepoHealth{}, nil); len(n) != 1 || n[0].ID != wantID {
		t.Errorf("after Keep mine: %+v", n)
	}
}

// Never persists only for the ids that offer it.
func TestNoticeDismissAllowlist(t *testing.T) {
	ts := serve(t, stateServer(t, newRepoDir(t, 1), t.TempDir()))
	for id, want := range map[string]int{
		noticeStaleLock: http.StatusOK, noticeCommitGraph: http.StatusOK, "feature_disabled_versions": http.StatusOK,
		noticeToolAgentUnsupportedPrefix + "0123456789ab": http.StatusOK,
		noticeToolTemplateUpdate:                          http.StatusBadRequest, "feature_disabled_../x": http.StatusBadRequest, "junk": http.StatusBadRequest,
	} {
		if code := postJSON(t, ts, "/api/notifications/dismiss", `{"id":"`+id+`"}`, "application/json", "", nil); code != want {
			t.Errorf("dismiss %q = %d, want %d", id, code, want)
		}
	}
}
