package exttool

import (
	"testing"

	"github.com/homeend/gigagit/internal/template"
)

// `gg review --model` appends the agent's model flag to its review command:
// every built-in review template (each variant, on either OS) must end in
// the agent's own arguments, or domain.ResolveReviewCommand refuses it.
func TestReviewTemplatesTakeAnAppendedModel(t *testing.T) {
	t.Parallel()
	seen := 0
	for _, tl := range Builtins() {
		if tl.ModelFlag == "" {
			continue
		}
		for _, ct := range tl.Commands {
			if ct.Category != CatReview {
				continue
			}
			for _, goos := range []string{"linux", "windows"} {
				seen++
				if sep := template.AppendBlockerFor(GenerateCommandFor(ct, tl.ID, goos), goos); sep != "" {
					t.Errorf("%s %q (%s, range %q): an appended model would land after %q", tl.ID, ct.Name, goos, ct.Range, sep)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no review template checked")
	}
}
