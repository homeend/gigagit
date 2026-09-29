package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// L on a two-branch row copies the preview link (the bookmark popup's key
// for the same thing); the hint line advertises it.
func TestVersionsPopupLCopiesThePreviewLink(t *testing.T) {
	t.Parallel()
	var copied string
	m := Model{width: 120, height: 40, linkRepoName: "gigagit"}
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	p := &versionsPopup{mode: versionsModeVersions, branch: "main", rows: []model.BranchVersion{twoBranchVersion()}}
	m = m.pushLayer(p)
	if !strings.Contains(m.View(), "[L] copy link") {
		t.Fatal("the hint line must advertise L")
	}
	mm, cmd := m.Update(keyMsg("L"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("L must return the copy command")
	}
	cmd()
	if want := "gg://gigagit@" + tvBase + ".." + tvOurs + "?version=1753100000-rebase"; copied != want {
		t.Fatalf("copied %q, want %q", copied, want)
	}
	if layerOf[*versionsPopup](m) == nil {
		t.Fatal("copying must leave the popup open")
	}
}

func TestVersionsPopupLOnAOneBranchRowSaysNoPreview(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40, linkRepoName: "gigagit"}
	v := twoBranchVersion()
	v.Base, v.Ours = "", ""
	p := &versionsPopup{mode: versionsModeVersions, branch: "main", rows: []model.BranchVersion{v}}
	m = m.pushLayer(p)
	mm, cmd := m.Update(keyMsg("L"))
	m = mm.(Model)
	if cmd != nil || m.statusMsg != i18n.T("this version records no preview") {
		t.Fatalf("status = %q cmd=%v, want the no-preview message and no copy", m.statusMsg, cmd)
	}
}

// . on a version row opens the action menu over the popup: the popup's own
// keys as rows (Copy link only when the row has a preview), and a row runs
// exactly what its key does, the popup left open under it.
func TestVersionsPopupDotMenu(t *testing.T) {
	t.Parallel()
	var copied string
	m := Model{width: 120, height: 40, linkRepoName: "gigagit"}
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	one := twoBranchVersion()
	one.Base, one.Ours = "", ""
	p := &versionsPopup{mode: versionsModeVersions, branch: "main", rows: []model.BranchVersion{twoBranchVersion(), one}}
	m = m.pushLayer(p)
	if !strings.Contains(m.View(), "[.] menu") {
		t.Fatal("the hint line must advertise .")
	}
	mm, _ := m.Update(keyMsg("."))
	m = mm.(Model)
	if m.actionMenu == nil {
		t.Fatal(". must open the action menu")
	}
	labels := func() []string {
		var out []string
		for _, r := range m.actionMenu.rows {
			out = append(out, r.label)
		}
		return out
	}
	want := []string{i18n.T("Open preview"), i18n.T("Restore version…"), i18n.T("Delete version…"), i18n.T("Copy commit sha"), i18n.T("Copy link")}
	if strings.Join(labels(), "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %v, want %v", labels(), want)
	}
	// The Copy link row: the same copy L makes.
	var cmd tea.Cmd
	var tm tea.Model
	tm, cmd = m.runVisibleRow(len(want) - 1)
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("Copy link must return the copy command")
	}
	cmd()
	if want := "gg://gigagit@" + tvBase + ".." + tvOurs + "?version=1753100000-rebase"; copied != want {
		t.Fatalf("copied %q, want %q", copied, want)
	}
	if m.actionMenu != nil || layerOf[*versionsPopup](m) == nil {
		t.Fatal("the menu closes and the popup stays")
	}
	// A one-branch row has no preview: no Copy link row.
	p.sel = 1
	mm, _ = m.Update(keyMsg("."))
	m = mm.(Model)
	for _, l := range labels() {
		if l == i18n.T("Copy link") {
			t.Fatal("a one-branch row must not offer Copy link")
		}
	}
	// The branch list has no menu.
	p.mode = versionsModeBranches
	m.actionMenu = nil
	mm, _ = m.Update(keyMsg("."))
	if mm.(Model).actionMenu != nil {
		t.Fatal("the branch list must not open a menu")
	}
}
