package exttool

import (
	"testing"

	"github.com/homeend/gigagit/internal/template"
)

// Every builtin template must validate once <bin> is generated away — a
// catalog typo must fail here, not in a user's config.
func TestBuiltinTemplateTokensValidate(t *testing.T) {
	for _, tl := range Builtins() {
		for _, ct := range tl.Commands {
			gen := GenerateCommand(ct, "tool")
			if err := template.ValidateCommandTokens(gen, ct.PerFile); err != nil {
				t.Errorf("%s/%s: %v", tl.ID, ct.Name, err)
			}
		}
	}
}
