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
