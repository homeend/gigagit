package web

import (
	"regexp"
	"testing"
)

// A console row's runs are inline spans, and an inline span paints its
// background over the font's glyph box only (~14px at 12px), not the 16px
// row: an agent that colours every cell's background (Junie) showed a
// stripe of the page between all its rows. Each run fills the row.
func TestConsoleRunsFillTheRowHeight(t *testing.T) {
	t.Parallel()
	rule := regexp.MustCompile(`#console-grid \.conrow > span \{([^}]*)\}`).FindStringSubmatch(readStatic(t, "style.css"))
	if rule == nil {
		t.Fatal("style.css: no `#console-grid .conrow > span` rule — run backgrounds leave a gap between rows")
	}
	for _, decl := range []string{`display:\s*inline-block`, `height:\s*16px`, `vertical-align:\s*top`} {
		if !regexp.MustCompile(decl).MatchString(rule[1]) {
			t.Errorf("style.css: `#console-grid .conrow > span` lacks %s", decl)
		}
	}
}
