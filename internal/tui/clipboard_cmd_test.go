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
	okMsg, text, ok := copyFileChoice("Copy file path", "dir/f.txt")
	if !ok || okMsg != "Copied path: dir/f.txt" || text != "dir/f.txt" {
		t.Errorf("path choice = (%q, %q, %v)", okMsg, text, ok)
	}
	okMsg, text, ok = copyFileChoice("Copy file name", "dir/f.txt")
	if !ok || okMsg != "Copied file name: f.txt" || text != "f.txt" {
		t.Errorf("name choice = (%q, %q, %v)", okMsg, text, ok)
	}
	for _, opt := range []string{"Cancel", "bogus"} {
		if _, _, ok := copyFileChoice(opt, "dir/f.txt"); ok {
			t.Errorf("%q must not map to a copy", opt)
		}
	}
}

func TestCopyFilePromptOpensModal(t *testing.T) {
	m := footerModel()
	m, _ = m.copyFilePrompt("dir/f.txt")
	if m.modal == nil {
		t.Fatal("copyFilePrompt should set the chooser modal")
	}
	if m.modal.req.ID != "copy-file" {
		t.Errorf("modal ID = %q, want copy-file", m.modal.req.ID)
	}
	if m.modal.req.Prompt != "Copy — dir/f.txt" {
		t.Errorf("prompt = %q", m.modal.req.Prompt)
	}
	if got := strings.Join(m.modal.req.Options, "|"); got != "Copy file path|Copy file name|Cancel" {
		t.Errorf("options = %q (Cancel must be last: esc maps to the last option)", got)
	}
	// The copy options resolve to a clipboard cmd; Cancel resolves to nothing.
	// Never RUN the cmd — it would write the real clipboard.
	for _, opt := range []string{"Copy file path", "Copy file name"} {
		if _, cmd := m.modal.onResolve(m, opt); cmd == nil {
			t.Errorf("%q should return a clipboard cmd", opt)
		}
	}
	if _, cmd := m.modal.onResolve(m, "Cancel"); cmd != nil {
		t.Error("Cancel should return no cmd")
	}
}
