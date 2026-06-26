package archtest

import (
	"os/exec"
	"strings"
	"testing"
)

// TestFrontendsDoNotImportGit: internal/tui and internal/cli must reach git —
// and the shelf content store — only through internal/domain, never by a direct
// import. cmd/gg and internal/app are the composition root / wiring layer and
// are exempt.
func TestFrontendsDoNotImportGit(t *testing.T) {
	forbidden := map[string]string{
		"github.com/homeend/gigagit/internal/git":        "frontends must reach git through internal/domain",
		"github.com/homeend/gigagit/internal/shelf":      "frontends must reach the shelf store through internal/domain",
		"github.com/homeend/gigagit/internal/bookmark":   "frontends must reach the bookmark store through internal/domain",
		"github.com/homeend/gigagit/internal/searchhist": "frontends must reach the search-history store through internal/domain",
		"github.com/homeend/gigagit/internal/profile":    "frontends must reach the profile store through internal/domain",
		"github.com/homeend/gigagit/internal/prefix":     "frontends must reach the prefix store through internal/domain",
	}
	for _, pkg := range []string{
		"github.com/homeend/gigagit/internal/tui",
		"github.com/homeend/gigagit/internal/cli",
	} {
		out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, pkg).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		for _, imp := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if why, bad := forbidden[imp]; bad {
				t.Errorf("%s directly imports %s — %s", pkg, imp, why)
			}
		}
	}
}
