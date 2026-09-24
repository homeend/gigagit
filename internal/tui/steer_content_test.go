package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// navRepo's a.txt carries an unstaged edit ("EDITED" on line 18): the viewer
// must show the DISK bytes, which HEAD does not have.
func TestSteerContentLinkOpensTheDiskContent(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	c := steer.Command{ID: "c-1", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID, Wait: true}
	nm, cmd := m.applySteer(c)
	nm = pumpAll(t, nm, cmd)
	fv := layerOf[*fileViewer](nm)
	if fv == nil {
		t.Fatal("no file viewer after a content-link navigate")
	}
	var text []string
	for _, l := range fv.p.lines {
		text = append(text, l.text)
	}
	if !strings.Contains(strings.Join(text, "\n"), "EDITED") {
		t.Errorf("popup shows %q, want the working-tree bytes (EDITED)", text)
	}
	if !strings.Contains(nm.View(), "EDITED") {
		t.Error("the screen does not show the file's disk content")
	}
	r, ok := steer.AwaitReply(nm.steerDir, "c-1", time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok", r, ok)
	}
}

func TestSteerContentLinkMissingFileFails(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	c := steer.Command{ID: "c-2", Cmd: "navigate", File: "gone.txt",
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID, Wait: true}
	nm, cmd := m.applySteer(c)
	runSteerCmd(t, cmd)
	if layerOf[*fileViewer](nm) != nil {
		t.Error("a missing file must not open the viewer")
	}
	r, ok := steer.AwaitReply(nm.steerDir, "c-2", time.Second)
	if !ok || r.OK || r.Error != "gone.txt is not in the working tree" {
		t.Fatalf("reply = %+v ok=%v, want ok:false and the not-in-working-tree message", r, ok)
	}
}

// The `gg open` launch path: the pure converter carries the hint through.
func TestStartAtContentLinkCarriesTheHint(t *testing.T) {
	t.Parallel()
	l, err := model.ParseLink("gg:///mnt/t/repo/a.txt?view=content")
	if err != nil {
		t.Fatal(err)
	}
	l.Repo.Abs, l.Path = "/mnt/t/repo", "a.txt" // what linknav.AtLink hands the launcher
	c, ok := steerCommandForLink(l)
	if !ok || c.File != "a.txt" || c.HintKind != model.ContentHintKind {
		t.Fatalf("steerCommandForLink = %+v, %v — want a.txt with the content hint", c, ok)
	}
}
