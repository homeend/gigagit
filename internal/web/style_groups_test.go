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
	// Dim wins over the frame: the stale/outdated rules come AFTER the frame rules.
	s := string(b)
	if strings.LastIndex(s, ".notebox.mf-") > strings.Index(s, ".notebox.stale {") {
		t.Errorf("a .notebox.mf-* rule follows .notebox.stale — a stale note would take the frame colour")
	}
}
