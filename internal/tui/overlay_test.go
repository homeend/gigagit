package tui

import (
	"strings"
	"testing"
)

func TestOverlayAtPlacesAtCoordinates(t *testing.T) {
	bg := strings.Join([]string{
		"aaaaaaaaaa",
		"bbbbbbbbbb",
		"cccccccccc",
		"dddddddddd",
	}, "\n")
	out := overlayAt(bg, "XX", 3, 1, 10, 4)
	lines := strings.Split(out, "\n")
	if lines[1] != "bbbXXbbbbb" {
		t.Errorf("row 1 = %q, want XX at col 3", lines[1])
	}
	if lines[0] != "aaaaaaaaaa" || lines[2] != "cccccccccc" {
		t.Error("rows outside the overlay must be untouched")
	}
}

func TestOverlayAtClampsNegativeAndOverflow(t *testing.T) {
	bg := "aaaa\nbbbb"
	out := overlayAt(bg, "XY", -5, -5, 4, 2)
	if lines := strings.Split(out, "\n"); lines[0] != "XYaa" {
		t.Errorf("negative coords must clamp to 0,0; row 0 = %q", lines[0])
	}
	// A row beyond the grid is dropped, not panicking.
	out = overlayAt(bg, "XY", 0, 5, 4, 2)
	if lines := strings.Split(out, "\n"); lines[0] != "aaaa" || lines[1] != "bbbb" {
		t.Errorf("out-of-grid overlay must leave bg unchanged: %q", out)
	}
}
