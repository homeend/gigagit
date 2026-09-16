package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
)

func TestCompareDiffCacheKeyRule(t *testing.T) {
	t.Parallel()
	commit := mustCommitEndpoint("aaa1111")
	commit2 := mustCommitEndpoint("bbb2222")
	work := model.WorkTreeEndpoint()

	// commit↔commit → cached (non-empty key)
	if k := compareDiffKey(commit, commit2, "a.go"); k == "" {
		t.Error("commit↔commit must be cached (non-empty key)")
	}
	// any live side → bypass (empty key)
	if k := compareDiffKey(commit, work, "a.go"); k != "" {
		t.Errorf("live endpoint must bypass cache, got key %q", k)
	}
	if k := compareDiffKey(work, commit, "a.go"); k != "" {
		t.Errorf("live endpoint must bypass cache, got key %q", k)
	}
}

// A diffMsg means the load completed, so the handler must clear the view's
// loading flag for every path. The compare path reuses the pre-built loading
// view (v := m.diffLayer()), so unless the handler clears it the body stays stuck
// on "(loading…)" forever even though the header shows real counts.
func TestDiffMsgClearsLoading(t *testing.T) {
	t.Parallel()
	v := &diffView{loading: true} // pre-built loading view, reused by the loader
	m := Model{}
	m = m.pushLayer(v)
	m.diffTag = "cmp:aaa:worktree:README.md"

	// the result view is the SAME pointer with content applied but loading still
	// true (mirrors loadCompareDiffCmd → applyDiff, which never clears loading).
	u, _ := m.Update(diffMsg{tag: "cmp:aaa:worktree:README.md", view: v})
	mm := u.(Model)

	if mm.diffLayer() == nil {
		t.Fatal("diffView must survive a matching diffMsg")
	}
	if mm.diffLayer().loading {
		t.Error("a completed diffMsg must clear loading; body would stay on \"(loading…)\"")
	}
}

// TestCompareTagDoesNotKeyOnAMovingRev pins the bug that plan 1a's Task 3
// surfaced: CacheTag() returns Hash verbatim, so an endpoint holding "HEAD"
// keyed the session diff cache on a name that moves. Since compareDiffKey
// bypasses the cache whenever either side is LIVE, the exposed pair is
// commit↔commit: re-opening one after HEAD advanced could be served the
// PREVIOUS diff.
//
// This builds the endpoint the way the production path now does (resolve
// HEAD through the domain service, THEN build the Endpoint — see
// resolveHeadEndpoint in diff_view.go, used by file_finder.go's "HEAD ↔
// working tree" action), at two different HEADs in a real repo, and requires
// the tags to differ. Before that fix, file_finder.go built its endpoint
// directly from the unresolved "HEAD" rev-spec — a moving name, not a
// resolved sha — and this test failed with both tags equal to the literal
// "HEAD" (see the task-3b report for the failing run).
func TestCompareTagDoesNotKeyOnAMovingRev(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hi\n")
	m := loadedModelAt(t, dir)

	resolveHeadTag := func(m Model) string {
		t.Helper()
		ep, err := m.resolveHeadEndpoint()
		if err != nil {
			t.Fatalf("resolveHeadEndpoint: %v", err)
		}
		return ep.CacheTag()
	}

	tag1 := resolveHeadTag(m)

	gittest.Run(t, dir, "commit", "--allow-empty", "-m", "second")
	m = loadedModelAt(t, dir) // re-read: a fresh Model, same as reopening the compare

	tag2 := resolveHeadTag(m)

	if tag1 == tag2 {
		t.Fatalf("CacheTag() must differ across commits when HEAD is resolved to a sha first, got %q for both", tag1)
	}
}

// TestResolveHeadEndpointSurvivesShortCoreAbbrev pins the core.abbrev
// regression. resolveHeadEndpoint used to read HEAD through
// svc.CommitLookup, i.e. `git log --format=%h`, whose width honours
// core.abbrev — legal all the way down to 4. model.CommitEndpoint requires
// 7..64, so on a repo with `core.abbrev = 4` every HEAD↔working-tree diff
// became a hard failure. It must resolve through svc.ResolveRev
// (`git rev-parse --verify`), which is always a full sha.
func TestResolveHeadEndpointSurvivesShortCoreAbbrev(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hi\n")
	gittest.Run(t, dir, "config", "core.abbrev", "4")
	m := loadedModelAt(t, dir)

	ep, err := m.resolveHeadEndpoint()
	if err != nil {
		t.Fatalf("resolveHeadEndpoint under core.abbrev=4: %v", err)
	}
	if len(ep.Hash()) < 40 {
		t.Fatalf("resolveHeadEndpoint must yield a FULL sha that core.abbrev cannot narrow, got %q", ep.Hash())
	}
}

func TestCompareEnterOpensDiff(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	if len(m.commits) == 0 {
		t.Skip("no commits")
	}
	left := mustCommitEndpoint(m.commits[0].Hash)
	m, _ = m.openCompareFiles(left, model.WorkTreeEndpoint())
	// apply the file list synchronously
	m.filesView.lines = []contentLine{{text: "M README.md", path: "README.md", status: "M"}}
	m.filesView.sel = 0
	m.filesTreeFocused = true

	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffLayer() == nil {
		t.Fatal("enter in compare mode must open the diff view")
	}
	if !strings.Contains(mm.diffTag, "README.md") {
		t.Errorf("diffTag = %q", mm.diffTag)
	}
	if cmd == nil {
		t.Fatal("expected a diff load command")
	}
}
