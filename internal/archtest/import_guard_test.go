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
		for _, imp := range directImports(t, pkg) {
			if why, bad := forbidden[imp]; bad {
				t.Errorf("%s directly imports %s — %s", pkg, imp, why)
			}
		}
	}
}

// TestLayeringDAG guards the rest of the layering: no package may import a
// package above (or beside-and-above) its stated layer. Direct imports only —
// transitive deps legitimately cross layers (tui→domain→git), so .Deps would
// false-positive; what this catches is someone wiring a new dependency edge
// backwards.
func TestLayeringDAG(t *testing.T) {
	cases := map[string][]string{
		"engine":      {"domain", "tui", "cli", "app"},
		"git":         {"engine", "domain", "tui", "cli", "app"},
		"gitcmd":      {"gitexec", "git", "engine", "domain", "tui", "cli", "app"},
		"gitconfdocs": {"git", "engine", "domain", "tui", "cli", "app"},
		"exttool":     {"git", "engine", "domain", "tui", "cli", "app"},
		"gitexec":     {"gitcmd", "git", "engine", "domain", "tui", "cli", "app"},
		"model":       {"repogate", "gitcmd", "gitexec", "git", "engine", "domain", "tui", "cli", "app"},
		"repogate":    {"git", "engine", "domain", "tui", "cli", "app"},
		"domain":      {"tui", "cli", "app"},
		"gitwatch":    {"git", "engine", "domain", "tui", "cli", "app"},
		"commitgraph": {"git", "engine", "domain", "tui", "cli", "app"},
		"promptstate": {"git", "engine", "domain", "tui", "cli", "app"},
		"textdiff":    {"git", "engine", "domain", "tui", "cli", "app"},
		"template":    {"git", "engine", "domain", "tui", "cli", "app"},
	}
	const root = "github.com/homeend/gigagit/internal/"
	for pkg, forbidden := range cases {
		bad := make(map[string]bool, len(forbidden))
		for _, f := range forbidden {
			bad[root+f] = true
		}
		for _, imp := range directImports(t, root+pkg) {
			if bad[imp] {
				t.Errorf("internal/%s directly imports %s — that edge points up the layering DAG", pkg, imp)
			}
		}
	}
}

// directImports returns pkg's direct (non-test) imports via go list.
func directImports(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, pkg).Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pkg, err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}
