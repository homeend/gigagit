package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestClipboardCopiedMsgSetsStatus(t *testing.T) {
	m := footerModel()
	u, _ := m.Update(clipboardCopiedMsg{ok: "Copied path: dir/f.txt"})
	if got := u.(Model).statusMsg; got != "Copied path: dir/f.txt" {
		t.Errorf("statusMsg = %q, want the ok message", got)
	}
}

func TestClipboardCopiedMsgError(t *testing.T) {
	m := footerModel()
	u, _ := m.Update(clipboardCopiedMsg{err: errors.New("boom")})
	if got := u.(Model).statusMsg; got != "copy failed: boom" {
		t.Errorf("statusMsg = %q, want \"copy failed: boom\"", got)
	}
}

func TestCopyFileChoice(t *testing.T) {
	const p, abs = "dir/f.txt", "/repo/dir/f.txt"
	okMsg, text, ok := copyFileChoice("Copy file path", p, abs)
	if !ok || okMsg != "Copied path: dir/f.txt" || text != "dir/f.txt" {
		t.Errorf("path choice = (%q, %q, %v)", okMsg, text, ok)
	}
	okMsg, text, ok = copyFileChoice("Copy absolute file path", p, abs)
	if !ok || okMsg != "Copied absolute path: /repo/dir/f.txt" || text != "/repo/dir/f.txt" {
		t.Errorf("abs choice = (%q, %q, %v)", okMsg, text, ok)
	}
	okMsg, text, ok = copyFileChoice("Copy file name", p, abs)
	if !ok || okMsg != "Copied file name: f.txt" || text != "f.txt" {
		t.Errorf("name choice = (%q, %q, %v)", okMsg, text, ok)
	}
	for _, opt := range []string{"Cancel", "bogus"} {
		if _, _, ok := copyFileChoice(opt, p, abs); ok {
			t.Errorf("%q must not map to a copy", opt)
		}
	}
}

func TestCopyFilePromptOpensModal(t *testing.T) {
	m := footerModel() // currentWorktree == "/repo"
	m, _ = m.copyFilePrompt("", "dir/f.txt")
	if m.modal == nil {
		t.Fatal("copyFilePrompt should set the chooser modal")
	}
	if m.modal.req.ID != "copy-file" {
		t.Errorf("modal ID = %q, want copy-file", m.modal.req.ID)
	}
	if m.modal.req.Prompt != "Copy — dir/f.txt" {
		t.Errorf("prompt = %q", m.modal.req.Prompt)
	}
	const wantOpts = "Copy file path|Copy absolute file path|Copy file name|Cancel"
	if got := strings.Join(m.modal.req.Options, "|"); got != wantOpts {
		t.Errorf("options = %q, want %q (Cancel last: esc maps to it)", got, wantOpts)
	}
	// The copy options resolve to a clipboard cmd; Cancel resolves to nothing.
	// Never RUN the cmd — it would write the real clipboard.
	for _, opt := range []string{"Copy file path", "Copy absolute file path", "Copy file name"} {
		if _, cmd := m.modal.onResolve(m, opt); cmd == nil {
			t.Errorf("%q should return a clipboard cmd", opt)
		}
	}
	if _, cmd := m.modal.onResolve(m, "Cancel"); cmd != nil {
		t.Error("Cancel should return no cmd")
	}
	// The inspectable seam: an empty base anchors on the current worktree.
	if got := m.modal.copyTexts["Copy absolute file path"]; got != "/repo/dir/f.txt" {
		t.Errorf("captured abs = %q, want /repo/dir/f.txt", got)
	}
}

func TestCopyFilePromptBaseAnchorsOnEntryWorktree(t *testing.T) {
	// A non-empty base (a cross-worktree bookmark) resolves the absolute option
	// against THAT base, not the current worktree — the crux of the design.
	m := footerModel() // currentWorktree == "/repo"
	m, _ = m.copyFilePrompt("/wt", "dir/f.txt")
	if got := m.modal.copyTexts["Copy absolute file path"]; got != "/wt/dir/f.txt" {
		t.Errorf("captured abs = %q, want /wt/dir/f.txt (entry's own worktree)", got)
	}
	if got := m.modal.copyTexts["Copy file path"]; got != "dir/f.txt" {
		t.Errorf("captured rel = %q, want dir/f.txt", got)
	}
}
