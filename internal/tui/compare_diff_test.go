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
