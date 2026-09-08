package tui

import (
	"os"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// TestMain turns notes off for the whole package. loadCmd and the
// configReadyMsg path call StartNotesSweep, so without this every test that
// loads would (a) resolve the USER's real state dir, and (b) run a background
// pass that probes the injected (often Fake) Runner off-thread. Pinning
// NotesStatePath to one shared dir is not enough: the first test to write a
// note would leave it there for every later parallel sweep to read.
//
// A test that means to exercise notes opts back in per Service:
//
//	svc.UseNotesDir(t.TempDir())
func TestMain(m *testing.M) {
	domain.NotesDisabled = true
	os.Exit(m.Run())
}
