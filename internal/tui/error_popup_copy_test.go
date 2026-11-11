package tui

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// y in the [E] viewer puts the WHOLE message on the clipboard — every line as
// git wrote it, not the rows the box wrapped it into — and says so inside the
// box, since a tall box covers the status bar.
func TestErrorViewerYCopiesTheWholeMessage(t *testing.T) {
	t.Parallel()
	m := sizedModel(t, 60, 30)
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	msg := "error: git push failed (exit 128): fatal: 'C:/Users/someone/scoop/apps/git/work/audiobookz/.git' does not appear to be a git repository\r\n" +
		"fatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n"
	m.lastError = msg
	tm, _ := m.openErrorPopup()
	m = tm.(Model)
	p := m.topLayer().(*contentPopup)

	if !strings.Contains(strings.Join(boxLines(t, p, m), "\n"), "[y] copy") {
		t.Fatalf("the viewer must advertise y:\n%s", strings.Join(boxLines(t, p, m), "\n"))
	}

	m, cmd := pressRuneKey(m, "y")
	if cmd == nil {
		t.Fatal("y returned no command")
	}
	tm, _ = m.Update(cmd())
	m = tm.(Model)

	want := "error: git push failed (exit 128): fatal: 'C:/Users/someone/scoop/apps/git/work/audiobookz/.git' does not appear to be a git repository\n" +
		"fatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists."
	if copied != want {
		t.Fatalf("clipboard:\n%q\nwant:\n%q", copied, want)
	}
	if m.topLayer() != layer(p) {
		t.Fatal("the viewer must stay open after a copy")
	}
	if !strings.Contains(strings.Join(boxLines(t, p, m), "\n"), "Copied the message") {
		t.Fatalf("the box must confirm the copy:\n%s", strings.Join(boxLines(t, p, m), "\n"))
	}
	if m.lastError != msg {
		t.Fatalf("a copy must not replace the message it copied: %q", m.lastError)
	}
}

// A clipboard that refuses says so in the box too — the status bar may be
// behind it.
func TestErrorViewerShowsAFailedCopy(t *testing.T) {
	t.Parallel()
	m := sizedModel(t, 80, 30)
	m.clipWrite = func(io.Writer, string) (string, error) { return "", errors.New("no clipboard") }
	m.lastError = "error: git push failed (exit 128)"
	tm, _ := m.openErrorPopup()
	m = tm.(Model)
	p := m.topLayer().(*contentPopup)
	m, cmd := pressRuneKey(m, "y")
	tm, _ = m.Update(cmd())
	m = tm.(Model)
	if !strings.Contains(strings.Join(boxLines(t, p, m), "\n"), "copy failed: no clipboard") {
		t.Fatalf("the box must report the failed copy:\n%s", strings.Join(boxLines(t, p, m), "\n"))
	}
}

// y belongs to the message viewer: the help window (the same popup type) has
// no "whole text" worth a clipboard and keeps y unbound.
func TestPlainContentPopupLeavesYUnbound(t *testing.T) {
	t.Parallel()
	m := sizedModel(t, 80, 30)
	called := false
	m.clipWrite = func(io.Writer, string) (string, error) { called = true; return "fake", nil }
	m = m.pushLayer(newContentPopup("Help", []contentLine{{text: "a row"}}))
	_, cmd := pressRuneKey(m, "y")
	if cmd != nil {
		cmd()
	}
	if called {
		t.Fatal("y copied from a popup that does not offer it")
	}
}
