package archtest

import (
	"os/exec"
	"strings"
	"testing"
)

// TestFrontendsDoNotImportGit: internal/tui and internal/cli must reach git
// only through internal/domain, never by a direct import. cmd/gg and
// internal/app are the composition root / wiring layer and are exempt.
func TestFrontendsDoNotImportGit(t *testing.T) {
	const forbidden = "github.com/gigagit/gg/internal/git"
	for _, pkg := range []string{
		"github.com/gigagit/gg/internal/tui",
		"github.com/gigagit/gg/internal/cli",
	} {
		out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, pkg).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		for _, imp := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if imp == forbidden {
				t.Errorf("%s directly imports %s — frontends must reach git through internal/domain", pkg, forbidden)
			}
		}
	}
}
