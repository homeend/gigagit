package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestCompareDiffCacheKeyRule(t *testing.T) {
	commit := model.Endpoint{Kind: model.EndpointCommit, Hash: "aaa"}
	commit2 := model.Endpoint{Kind: model.EndpointCommit, Hash: "bbb"}
	work := model.Endpoint{Kind: model.EndpointWorkTree}

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
// view (v := m.diffView), so unless the handler clears it the body stays stuck
// on "(loading…)" forever even though the header shows real counts.
func TestDiffMsgClearsLoading(t *testing.T) {
	v := &diffView{loading: true} // pre-built loading view, reused by the loader
	m := Model{diffView: v, diffTag: "cmp:aaa:worktree:README.md"}

	// the result view is the SAME pointer with content applied but loading still
	// true (mirrors loadCompareDiffCmd → applyDiff, which never clears loading).
	u, _ := m.Update(diffMsg{tag: "cmp:aaa:worktree:README.md", view: v})
	mm := u.(Model)

	if mm.diffView == nil {
		t.Fatal("diffView must survive a matching diffMsg")
	}
	if mm.diffView.loading {
		t.Error("a completed diffMsg must clear loading; body would stay on \"(loading…)\"")
	}
}

func TestCompareEnterOpensDiff(t *testing.T) {
	m := loadedModel(t)
	if len(m.commits) == 0 {
		t.Skip("no commits")
	}
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[0].Hash}
	m, _ = m.openCompareFiles(left, model.Endpoint{Kind: model.EndpointWorkTree})
	// apply the file list synchronously
	m.filesView.lines = []contentLine{{text: "M README.md", path: "README.md", status: "M"}}
	m.filesView.sel = 0
	m.filesTreeFocused = true

	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffView == nil {
		t.Fatal("enter in compare mode must open the diff view")
	}
	if !strings.Contains(mm.diffTag, "README.md") {
		t.Errorf("diffTag = %q", mm.diffTag)
	}
	if cmd == nil {
		t.Fatal("expected a diff load command")
	}
}
