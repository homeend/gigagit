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
//
// It ALSO points XDG_CONFIG_HOME at an empty directory for the whole package.
// Every model built through loadCmd reads config.DefaultGlobalPath(), so on a
// machine whose real global config sets, say, [ui] theme = "light", the
// serial settings tests applied that theme to the process-global styles and
// never restored it — every parallel test reading st() afterwards then saw the
// developer's colours (window_syntax_test.go fails exactly that way). The same
// leak would let a user's language, wheel_step or footer_actions steer the
// suite. Tests that need their own config dir still override the variable with
// t.Setenv.
//
// Merge previews get the same treatment as notes for the same reason:
// srcPreviews is part of the all-source fan-out, so without this every loading
// test would resolve the USER's real state dir. A test that means to exercise
// previews opts back in with svc.UsePreviewsDir(t.TempDir()).
func TestMain(m *testing.M) {
	domain.NotesDisabled = true
	domain.PreviewsDisabled = true
	domain.ForgeDisabled = true // no test may shell out to the real gh; see pr_read_serial_test.go
	dir, err := os.MkdirTemp("", "gg-tui-xdg")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
