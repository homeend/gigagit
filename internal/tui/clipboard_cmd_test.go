package tui

import (
	"errors"
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
