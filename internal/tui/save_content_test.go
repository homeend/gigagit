package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/model"
)

// The whole point: a popup's text has to leave the box without the mouse.
// tmux selects full terminal-width lines, so the panels either side of a
// centred popup come along with any hand selection.
func TestContentPopupSaveWritesTheText(t *testing.T) {
	m := sizedModel(t, 100, 40)
	body := []string{
		"sudo sh -c 'mkdir -p /etc/binfmt.d && \\",
		"  echo \":WSLInterop:M::MZ::/init:PF\" > /etc/binfmt.d/WSLInterop.conf'",
	}
	var lines []contentLine
	for _, l := range body {
		lines = append(lines, contentLine{text: l})
	}
	m = m.pushLayer(newContentPopup("Full message", lines))

	u, cmd := m.Update(keyMsg("s"))
	m = u.(Model)
	if cmd == nil {
		t.Fatal("s must return a command that writes the file")
	}
	msg, ok := cmd().(contentSavedMsg)
	if !ok {
		t.Fatalf("want contentSavedMsg, got %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("save failed: %v", msg.err)
	}
	t.Cleanup(func() { os.Remove(msg.path) })

	got, err := os.ReadFile(msg.path)
	if err != nil {
		t.Fatal(err)
	}
	// The RAW text, not the wrapped render: a command split across two rows in
	// the box must come back as one pasteable line.
	if want := strings.Join(body, "\n") + "\n"; string(got) != want {
		t.Fatalf("file content =\n%q\nwant\n%q", got, want)
	}
}

// The path reaches the user, or the file might as well not exist.
func TestSavedPathIsReported(t *testing.T) {
	m := sizedModel(t, 100, 40)
	u, _ := m.Update(contentSavedMsg{path: "/tmp/gg-notice-123.txt"})
	if got := u.(Model).statusMsg; !strings.Contains(got, "/tmp/gg-notice-123.txt") {
		t.Fatalf("status must name the file, got %q", got)
	}
}

// A failed write says so rather than reporting a path that isn't there.
func TestSaveFailureIsReported(t *testing.T) {
	m := sizedModel(t, 100, 40)
	u, _ := m.Update(contentSavedMsg{err: os.ErrPermission})
	got := u.(Model).statusMsg
	if !strings.Contains(got, "permission denied") {
		t.Fatalf("status must carry the failure, got %q", got)
	}
	if !statusIsError(got) {
		t.Fatalf("a failed save must read as an error, got %q", got)
	}
}

// The notice dialog is where this was actually hit — the WSL-interop notice
// carries a multi-line sudo block. Its save covers the title and the detail.
func TestNoticePopupSaveWritesTitleAndDetail(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: model.RepoHealth{GitCommonDir: "/k"}, clipAvail: x11NoToolAvail()})
	m = nm.(Model)
	if len(m.notices) == 0 {
		t.Fatal("precondition: a notice must be present")
	}
	u, _ := m.Update(keyMsg("!"))
	m = u.(Model)
	if layerOf[*noticePopup](m) == nil {
		t.Fatal("precondition: ! must open the notification dialog")
	}

	u, cmd := m.Update(keyMsg("s"))
	if cmd == nil {
		t.Fatal("s must return a command that writes the file")
	}
	msg, ok := cmd().(contentSavedMsg)
	if !ok {
		t.Fatalf("want contentSavedMsg, got %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("save failed: %v", msg.err)
	}
	t.Cleanup(func() { os.Remove(msg.path) })

	got, err := os.ReadFile(msg.path)
	if err != nil {
		t.Fatal(err)
	}
	n := m.notices[0]
	if !strings.Contains(string(got), n.title) {
		t.Fatalf("saved text must carry the notice title %q:\n%s", n.title, got)
	}
	for _, d := range n.detail {
		if d == "" {
			continue
		}
		if !strings.Contains(string(got), d) {
			t.Fatalf("saved text must carry the detail line %q:\n%s", d, got)
		}
	}
	_ = u
}

// A popup box tall enough to cover the status bar hides the very path the save
// just produced — so the confirmation has to render INSIDE the box too, not
// only on the status line underneath it.
func TestSavedPathIsVisibleInsideThePopup(t *testing.T) {
	m := sizedModel(t, 100, 40)
	m = m.pushLayer(newContentPopup("Full message", []contentLine{{text: "hello"}}))
	u, _ := m.Update(contentSavedMsg{path: "/tmp/gg-full-message-42.txt"})
	m = u.(Model)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "/tmp/gg-full-message-42.txt") {
		t.Fatalf("the popup must show where it saved:\n%s", out)
	}
}

func TestSavedPathIsVisibleInsideTheNoticeDialog(t *testing.T) {
	m, _ := noticeTestModel(t)
	u, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: model.RepoHealth{GitCommonDir: "/k"}, clipAvail: x11NoToolAvail()})
	m = u.(Model)
	u, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = u.(Model)
	u, _ = m.Update(keyMsg("!"))
	m = u.(Model)
	u, _ = m.Update(contentSavedMsg{path: "/tmp/gg-notice-42.txt"})
	if out := ansi.Strip(u.(Model).View()); !strings.Contains(out, "/tmp/gg-notice-42.txt") {
		t.Fatalf("the notification dialog must show where it saved:\n%s", out)
	}
}

// Advertised in the footer of both popups, or nobody finds it.
func TestSaveHintIsAdvertised(t *testing.T) {
	m := sizedModel(t, 100, 40)
	cp := m.pushLayer(newContentPopup("Full message", []contentLine{{text: "hi"}}))
	if out := ansi.Strip(cp.View()); !strings.Contains(out, "[s] save") {
		t.Fatalf("the content viewer must advertise [s]:\n%s", out)
	}

	nm, _ := noticeTestModel(t)
	u, _ := nm.Update(repoHealthMsg{gen: nm.noticeGen, health: model.RepoHealth{GitCommonDir: "/k"}, clipAvail: x11NoToolAvail()})
	nm = u.(Model)
	u, _ = nm.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	nm = u.(Model)
	u, _ = nm.Update(keyMsg("!"))
	if out := ansi.Strip(u.(Model).View()); !strings.Contains(out, "[s] save") {
		t.Fatalf("the notification dialog must advertise [s]:\n%s", out)
	}
}
