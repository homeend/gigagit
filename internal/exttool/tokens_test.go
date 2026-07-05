package exttool

import (
	"testing"

	"github.com/homeend/gigagit/internal/template"
)

// Every builtin template must validate once <bin> and <env:...> are
// generated away, on EVERY OS a template might be generated for — a catalog
// typo (or a leftover <env:...> the runtime resolver would reject) must fail
// here, not in a user's config.
func TestBuiltinTemplateTokensValidate(t *testing.T) {
	for _, tl := range Builtins() {
		for _, ct := range tl.Commands {
			for _, goos := range []string{"linux", "windows"} {
				gen := GenerateCommandFor(ct, "tool", goos)
				if err := template.ValidateCommandTokens(gen, ct.PerFile); err != nil {
					t.Errorf("%s/%s (%s): %v", tl.ID, ct.Name, goos, err)
				}
			}
		}
	}
}
