package web

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/theme"
)

// The web's six group colours are the TUI dark theme's: a review keeps one
// colour in both frontends.
func TestWebGroupColoursAreTheDarkTheme(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{theme.Dark.NoteGroup1, theme.Dark.NoteGroup2, theme.Dark.NoteGroup3,
		theme.Dark.NoteGroup4, theme.Dark.NoteGroup5, theme.Dark.NoteGroup6}
	for i, c := range want {
		m := regexp.MustCompile(fmt.Sprintf(`--note-group-%d:\s*([^;]+);`, i+1)).FindStringSubmatch(string(b))
		if m == nil || !strings.EqualFold(strings.TrimSpace(m[1]), c) {
			t.Errorf("--note-group-%d = %v, want %s", i+1, m, c)
		}
	}
}

// The meta colours (spec 2026-10-12-note-meta §6.4/§7) are the TUI's Dark
// meta_* roles: the web has one palette, the dark one.
func TestWebMetaColoursAreTheDarkTheme(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	d := theme.Dark
	for name, c := range map[string]string{"red": d.MetaRed, "orange": d.MetaOrange, "yellow": d.MetaYellow,
		"green": d.MetaGreen, "blue": d.MetaBlue, "purple": d.MetaPurple, "grey": d.MetaGrey} {
		m := regexp.MustCompile(`--meta-` + name + `:\s*([^;]+);`).FindStringSubmatch(string(b))
		if m == nil || !strings.EqualFold(strings.TrimSpace(m[1]), c) {
			t.Errorf("--meta-%s = %v, want %s", name, m, c)
		}
		for _, sel := range []string{".mc-" + name + ".ms-filled", ".mc-" + name + ".ms-text", ".notebox.mf-" + name, ".vnote.mf-" + name} {
			if !strings.Contains(string(b), sel) {
				t.Errorf("style.css lacks %s", sel)
			}
		}
	}
	// The order (frame over kind, dim over frame) is TestWebMetaFrameOrder's.
}

// A declared frame beats the kind colours (a review remark is a read-only
// "forge" box; an agent box is purple) — and dim still beats the frame, on
// the diff's boxes and the viewer's (spec 2026-10-12-note-meta §6.1).
func TestWebMetaFrameOrder(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	lastFrame := strings.LastIndex(s, ".notebox.mf-")
	for _, kind := range []string{".notebox.forge {", ".notebox.agent {"} {
		if i := strings.Index(s, kind); i < 0 || i > lastFrame {
			t.Errorf("%s comes after the .notebox.mf-* rules: it would hide a declared frame", kind)
		}
	}
	dim := regexp.MustCompile(`\.notebox\.stale\[class\*=" mf-"\], \.notebox\.outdated\[class\*=" mf-"\] \{ border-color: var\(--dim\); \}`).FindStringIndex(s)
	if dim == nil || dim[0] < lastFrame {
		t.Errorf("no stale/outdated dim border rule after the .notebox.mf-* rules")
	}
	vFrame := strings.LastIndex(s, ".vnote.mf-")
	vDim := regexp.MustCompile(`\.vnote\.outdated \{ border-color: var\(--dim\); \}`).FindStringIndex(s)
	if vDim == nil || vDim[0] < vFrame {
		t.Errorf("an outdated viewer note with a frame must still draw a dim border")
	}
}
