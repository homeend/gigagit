package web

import (
	"strings"
	"testing"
)

// ←/→ pan a diff's long lines in scroll mode (the TUI's left/right in its
// diff view): the pan lives in the per-side .hbar scrollers, which the
// browser's own arrow scroll never reaches, so the keys must drive them —
// in the one-file diff, a stack, and the file-history overlay alike.
func TestDiffArrowsPanTheBars(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want, why string }{
		{"files.js", "function panBarsKey(", "one helper pans every bar in a scope"},
		{"files.js", "if (panBarsKey(", "the diff layout's arrows pan its bars"},
		{"filehist.js", "panBarsKey(histDiffEl(), e)", "the history overlay's diff pans too"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
