package tui

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

const switcherSha = "0123456789abcdef0123456789abcdef01234567"

// switcherLinkModel is a Model over a real (empty) repo, with a slice for a
// clipboard. The rows under test are hand-built: what L copies is a pure
// function of the row.
func switcherLinkModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	m := New(domain.New(newRepo(t)))
	m.linkRepoName = "r"
	m.svc.UseLinkHistDir(t.TempDir())
	var wrote []string
	m.clipWrite = func(_ io.Writer, s string) (string, error) {
		wrote = append(wrote, s)
		return "fake", nil
	}
	return m, &wrote
}

func pressL(t *testing.T, m Model) Model {
	t.Helper()
	nm, cmd := m.Update(keyMsg("L"))
	runCmds(cmd)
	return nm.(Model)
}

func TestLCopiesABookmarksLink(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		b    model.Bookmark
		want string
	}{
		{"file at a commit", model.Bookmark{ID: "bk1", Path: "a.go", State: model.StateCommitted, Commit: switcherSha}, "gg://r/a.go@" + switcherSha + "?bookmark=bk1"},
		{"commit pointer", model.Bookmark{ID: "bk2", State: model.StateCommitted, Commit: switcherSha}, "gg://r@" + switcherSha + "?bookmark=bk2"},
		{"working-tree file", model.Bookmark{ID: "bk3", Path: "a.go", State: model.StateUnstaged}, "gg://r/a.go?bookmark=bk3"},
	} {
		m, wrote := switcherLinkModel(t)
		m = m.pushLayer(newBookmarkPopup([]model.Bookmark{c.b}))
		pressL(t, m)
		if len(*wrote) != 1 || (*wrote)[0] != c.want {
			t.Errorf("%s: wrote %v, want %q", c.name, *wrote, c.want)
			continue
		}
		if l, err := model.ParseLink(c.want); err != nil || l.Hint.Kind != "bookmark" {
			t.Errorf("%s: does not reparse with its hint: %+v, %v", c.name, l.Hint, err)
		}
	}
}

func TestLCopiesAShelfEntrysLink(t *testing.T) {
	t.Parallel()
	worktree := model.ShelfEntry{ID: "sh1", Kind: model.ShelfKindFile, Origin: model.FileAddress{Path: "a.go", State: model.StateUnstaged}}
	staged := model.ShelfEntry{ID: "sh2", Kind: model.ShelfKindFile, Origin: model.FileAddress{Path: "a.go", State: model.StateStaged}}
	commit := model.ShelfEntry{ID: "sh3", Kind: model.ShelfKindCommit, Origin: model.FileAddress{State: model.StateCommitted, Commit: switcherSha}}
	want := map[string]string{
		"sh1": "gg://r/a.go?shelf=sh1",
		"sh2": "gg://r/a.go@staged?shelf=sh2",
		"sh3": "gg://r@" + switcherSha + "?shelf=sh3",
	}
	// The working-tree and index rows must DIFFER, or the assertion below
	// cannot tell a row that ignored Origin.State from one that read it.
	if want["sh1"] == want["sh2"] {
		t.Fatal("fixture broken")
	}
	for _, e := range []model.ShelfEntry{worktree, staged, commit} {
		m, wrote := switcherLinkModel(t)
		m = m.pushLayer(newShelfPopup([]model.ShelfEntry{e}))
		pressL(t, m)
		if len(*wrote) != 1 || (*wrote)[0] != want[e.ID] {
			t.Errorf("%s: wrote %v, want %q", e.ID, *wrote, want[e.ID])
			continue
		}
		l, err := model.ParseLink((*wrote)[0])
		if err != nil || l.Hint.Kind != "shelf" || l.Hint.ID != e.ID {
			t.Errorf("%s: hint = %+v, %v", e.ID, l.Hint, err)
		}
		// The §3.3 exception is actually REACHED: a shelved working-tree or
		// index file addresses the SHELF snapshot, not the live tree.
		if e.Kind == model.ShelfKindFile {
			ep, err := m.svc.EndpointForLink(context.Background(), l)
			if err != nil || ep.Kind() != model.EndpointShelf || ep.ShelfID() != e.ID {
				t.Errorf("%s: endpoint = %v (%v), want the shelf entry", e.ID, ep.Kind(), err)
			}
		}
	}
}

func TestLIsInertWhilePickingACompareTarget(t *testing.T) {
	t.Parallel()
	m, wrote := switcherLinkModel(t)
	bp := newBookmarkPopup([]model.Bookmark{{ID: "bk1", Path: "a.go", State: model.StateCommitted, Commit: switcherSha}})
	bp.compareRef = &model.FileRef{Path: "x"}
	got := pressL(t, m.pushLayer(bp))
	sp := newShelfPopup([]model.ShelfEntry{{ID: "sh3", Kind: model.ShelfKindCommit, Origin: model.FileAddress{State: model.StateCommitted, Commit: switcherSha}}})
	sp.compareRef = &model.FileRef{Path: "x"}
	got2 := pressL(t, m.pushLayer(sp))
	if len(*wrote) != 0 || got.statusMsg != "" || got2.statusMsg != "" {
		t.Fatalf("wrote=%v status=%q/%q", *wrote, got.statusMsg, got2.statusMsg)
	}
}

// A bookmark OF a shelf entry has no link form (linkFor refuses StateShelf).
func TestLOnARowWithNoLinkFormSaysSo(t *testing.T) {
	t.Parallel()
	m, wrote := switcherLinkModel(t)
	m = m.pushLayer(newBookmarkPopup([]model.Bookmark{{ID: "bk9", Path: "a.go", State: model.StateShelf, ShelfID: "sh1"}}))
	got := pressL(t, m)
	if len(*wrote) != 0 || !strings.Contains(got.statusMsg, "no gg link") {
		t.Fatalf("wrote=%v status=%q", *wrote, got.statusMsg)
	}
}
