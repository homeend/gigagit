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
		"github.com/homeend/gigagit/internal/git":          "frontends must reach git through internal/domain",
		"github.com/homeend/gigagit/internal/shelf":        "frontends must reach the shelf store through internal/domain",
		"github.com/homeend/gigagit/internal/bookmark":     "frontends must reach the bookmark store through internal/domain",
		"github.com/homeend/gigagit/internal/notes":        "frontends must reach the note store through internal/domain",
		"github.com/homeend/gigagit/internal/preview":      "frontends must reach the preview store through internal/domain",
		"github.com/homeend/gigagit/internal/searchhist":   "frontends must reach the search-history store through internal/domain",
		"github.com/homeend/gigagit/internal/profile":      "frontends must reach the profile store through internal/domain",
		"github.com/homeend/gigagit/internal/prefix":       "frontends must reach the prefix store through internal/domain",
		"github.com/homeend/gigagit/internal/linkhist":     "frontends must reach the copied-link history store through internal/domain",
		"github.com/homeend/gigagit/internal/savedcompare": "frontends must reach the saved-comparison store through internal/domain",
	}
	for _, pkg := range []string{
		"github.com/homeend/gigagit/internal/tui",
		"github.com/homeend/gigagit/internal/cli",
		"github.com/homeend/gigagit/internal/mcp",
		"github.com/homeend/gigagit/internal/web",
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
		"engine":      {"domain", "tui", "cli", "mcp", "web", "app"},
		"git":         {"engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"gitcmd":      {"gitexec", "git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"gitconfdocs": {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"exttool":     {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"gitexec":     {"gitcmd", "git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"model":       {"repogate", "gitcmd", "gitexec", "git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"notebatch":   {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"repogate":    {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"mcp":         {"tui", "cli", "app", "web"},
		// "steer" is forbidden to domain on purpose: the link resolver's
		// ResolveOpts.LiveFn seam exists precisely so domain can ask "is a gg
		// session live here?" without taking that dependency.
		"domain":      {"tui", "cli", "mcp", "web", "app", "steer"},
		"gitwatch":    {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"i18n":        {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"commitgraph": {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"promptstate": {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"textdiff":    {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"syntax":      {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"steer":       {"config", "model", "git", "engine", "domain", "tui", "cli", "mcp", "web", "app", "gitwatch", "i18n", "theme"},
		// linknav sits between domain and the frontends: the one link→navigate
		// builder cli AND tui share, so it may reach neither.
		"linknav":  {"tui", "cli", "mcp", "web", "app"},
		"template": {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
		"theme":    {"config", "git", "engine", "domain", "tui", "cli", "mcp", "web", "app", "syntax", "i18n"},
		"web":      {"tui", "cli", "mcp", "app"},
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

// TestSteerIsAStdlibLeaf pins internal/steer's dependency budget: the steering
// protocol is imported by all four frontends AND by the CLI that talks to them,
// so any gg package it pulled in would become a dependency of everything.
// fsnotify is the one exception (the inbox wake).
func TestSteerIsAStdlibLeaf(t *testing.T) {
	t.Parallel()
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/steer") {
		if imp == "github.com/fsnotify/fsnotify" {
			continue
		}
		if first := strings.SplitN(imp, "/", 2)[0]; strings.Contains(first, ".") {
			t.Errorf("internal/steer imports %s — it must stay stdlib + fsnotify only", imp)
		}
	}
}

// TestReposIsAStdlibLeaf pins internal/repos's dependency budget. The registry
// is read by all four frontends AND (since gg links) by internal/domain, so
// any gg package it pulled in would become a dependency of everything — and
// the reason `Touch` takes the remote NAME rather than computing it is exactly
// that this package must never learn what a remote is. go-toml is the one
// exception (the on-disk format).
func TestReposIsAStdlibLeaf(t *testing.T) {
	t.Parallel()
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/repos") {
		if imp == "github.com/pelletier/go-toml/v2" {
			continue
		}
		if first := strings.SplitN(imp, "/", 2)[0]; strings.Contains(first, ".") {
			t.Errorf("internal/repos imports %s — it must stay stdlib + go-toml only", imp)
		}
	}
}

// TestPreflightIsAStdlibLeaf pins internal/preflight's dependency budget: it
// resolves feature requirements against probe results handed to it by
// callers, and must never grow a dependency on git or any gg package so it
// stays trivially testable with plain values.
func TestPreflightIsAStdlibLeaf(t *testing.T) {
	t.Parallel()
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/preflight") {
		if first := strings.SplitN(imp, "/", 2)[0]; strings.Contains(first, ".") {
			t.Errorf("internal/preflight imports %s — it must stay stdlib only", imp)
		}
	}
}

// TestChangesetIsAStdlibLeaf pins internal/changeset's dependency budget: it
// compares two base-relative change sets and must never grow a dependency on
// git or any gg package so it stays trivially testable with plain values.
func TestChangesetIsAStdlibLeaf(t *testing.T) {
	t.Parallel()
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/changeset") {
		if first := strings.SplitN(imp, "/", 2)[0]; strings.Contains(first, ".") {
			t.Errorf("internal/changeset imports %s — it must stay stdlib only", imp)
		}
	}
}

// TestBranchfilterIsStdlibOnly pins internal/branchfilter's dependency
// budget: it is the one evaluator behind the branch-filter slots, shared by
// config, domain, the TUI, and the web server, so it must never grow a
// dependency on git or any gg package.
func TestBranchfilterIsStdlibOnly(t *testing.T) {
	t.Parallel()
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/branchfilter") {
		if first := strings.SplitN(imp, "/", 2)[0]; strings.Contains(first, ".") {
			t.Errorf("internal/branchfilter imports %s — it must stay stdlib only", imp)
		}
	}
}

// TestFilelockIsAStdlibLeaf pins internal/filelock's dependency budget. It is
// the ONE cross-process lock behind every per-repo state file — notes,
// previews and the link history all delegate to it — precisely so a fix (like
// the Windows ErrPermission retry) lands once instead of in three copies. A
// dependency here would be a dependency of all three stores at once, and the
// package has to stay trivially testable with a bare temp dir.
//
// Unlike the stores above it is NOT in the frontend-import ban: it is a
// generic utility like internal/cache, not a store holding user data.
func TestFilelockIsAStdlibLeaf(t *testing.T) {
	t.Parallel()
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/filelock") {
		if first := strings.SplitN(imp, "/", 2)[0]; strings.Contains(first, ".") {
			t.Errorf("internal/filelock imports %s — it must stay stdlib only", imp)
		}
	}
}

// TestSavedCompareIsALeaf pins internal/savedcompare's dependency budget: a
// records-only registry of saved comparisons, owned by internal/domain. Like
// linkhist it takes an explicit root — XDG resolution is domain's job — so it
// must never reach for internal/config or internal/git to find its own
// directory. Its whole budget is stdlib, internal/model (the gg:// Link it
// stores), the shared file lock, and the same TOML library every other store
// already uses.
//
// internal/model in particular: an entry holds model.Link values, not link
// STRINGS, so the package that owns the grammar is a legitimate dependency
// here where it would not be for linkhist (which stores raw text).
func TestSavedCompareIsALeaf(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{
		"github.com/homeend/gigagit/internal/filelock": true,
		"github.com/homeend/gigagit/internal/model":    true,
		"github.com/pelletier/go-toml/v2":              true,
	}
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/savedcompare") {
		if allowed[imp] {
			continue
		}
		if first := strings.SplitN(imp, "/", 2)[0]; strings.Contains(first, ".") {
			t.Errorf("internal/savedcompare imports %s — only stdlib, internal/model, internal/filelock and go-toml are allowed", imp)
		}
	}
}

// TestLinkhistIsALeaf pins internal/linkhist's dependency budget: a
// records-only MRU of copied gg:// links, owned by internal/domain. It takes
// an explicit root — XDG resolution is domain's job, and that is where the
// project's "a new store checks XDG_STATE_HOME first" rule is satisfied — so
// linkhist must never reach for internal/config or internal/git to find its
// own directory. Its whole budget is stdlib, the shared file lock, and the
// same TOML library every other store already uses.
func TestLinkhistIsALeaf(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{
		"github.com/homeend/gigagit/internal/filelock": true,
		"github.com/pelletier/go-toml/v2":              true,
	}
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/linkhist") {
		if allowed[imp] {
			continue
		}
		if first := strings.SplitN(imp, "/", 2)[0]; strings.Contains(first, ".") {
			t.Errorf("internal/linkhist imports %s — only stdlib, internal/filelock and go-toml are allowed", imp)
		}
	}
}
