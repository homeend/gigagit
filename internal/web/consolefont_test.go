package web

import (
	"regexp"
	"strings"
	"testing"
)

// A shell prompt draws Nerd Font icons (folder, branch, clock) that no
// programming font has; the console finds them only through the Nerd Font
// names at the end of its font list. polish.css loads after style.css, so
// the LAST #console-grid font rule across both decides — it once replaced
// the list with the plain monospace one and every icon became a hex box.
func TestConsoleGridKeepsNerdFontFallbacks(t *testing.T) {
	t.Parallel()
	style := readStatic(t, "style.css")
	if !regexp.MustCompile(`--nerd-fonts:[^;]*"MesloLGS NF"[^;]*"Symbols Nerd Font Mono"`).MatchString(style) {
		t.Fatal("style.css: no --nerd-fonts token naming the Nerd Font fallbacks")
	}
	comments := regexp.MustCompile(`(?s)/\*.*?\*/`)
	rule := regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
	font := regexp.MustCompile(`(?:^|;)\s*font(?:-family)?\s*:([^;]*)`)
	last, where := "", ""
	for _, f := range []string{"style.css", "polish.css"} { // load order
		for _, m := range rule.FindAllStringSubmatch(comments.ReplaceAllString(readStatic(t, f), ""), -1) {
			hit := false
			for _, sel := range strings.Split(m[1], ",") {
				hit = hit || strings.TrimSpace(sel) == "#console-grid"
			}
			if fm := font.FindStringSubmatch(m[2]); hit && fm != nil {
				last, where = fm[1], f
			}
		}
	}
	if !strings.Contains(last, "var(--nerd-fonts)") {
		t.Errorf("%s: #console-grid's font list %q drops var(--nerd-fonts) — prompt icons draw as hex boxes", where, strings.TrimSpace(last))
	}
}
