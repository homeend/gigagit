package tui

import (
	"os"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// TestMain points the notes store at a throwaway directory for the whole
// package. loadCmd and the configReadyMsg path now call StartNotesSweep, and
// without this override the background pass would resolve the USER's real
// state dir — and probe the injected (often Fake) Runner for the git common
// dir off-thread, racing tests that reconfigure that Runner afterwards. With
// NotesStatePath set, notesStore skips the probe entirely and the sweep is a
// no-op on a store file that never exists.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gg-tui-notes")
	if err != nil {
		panic(err)
	}
	domain.NotesStatePath = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
