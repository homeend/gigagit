package tui

import (
	"io"
	"strings"
	"testing"

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
