package web

import (
	"os/exec"
	"strings"
	"testing"
)

// nodeRun runs src (a module body) under node and returns its trimmed output.
func nodeRun(t *testing.T, src string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	cmd := exec.Command(node, "--input-type=module", "-e", src)
	cmd.Env = append(cmd.Environ(), "TZ=UTC")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// The TUI stash window's / filter: a case-insensitive substring of
// "<ref>  <subject>".
func TestStashFilterJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "sidebar.js", "stashVisible") + `
const es = [{ ref: "stash@{0}", subject: "WIP on main: fix Parser" }, { ref: "stash@{1}", subject: "On feat: docs" }, { ref: "stash@{2}" }];
console.log(JSON.stringify([es, "parser", "STASH@{1}", "", "0}  wip", "nothing"].slice(1).map((q) => stashVisible(es, q).map((e) => e.ref))));
`
	if got := nodeRun(t, src); got != `[["stash@{0}"],["stash@{1}"],["stash@{0}","stash@{1}","stash@{2}"],["stash@{0}"],[]]` {
		t.Errorf("stashVisible = %s", got)
	}
}

func TestStashFilterWired(t *testing.T) {
	t.Parallel()
	sb := readStaticSrc(t, "sidebar.js")
	for _, want := range []string{
		`stashVisible(state.stashes, state.stashQuery)`,
		`title="filter the stashes (/ in the TUI)"`,
		`if (n === "stashes") {`,
		`$("stash-filter").addEventListener("keydown"`,
	} {
		if !strings.Contains(sb, want) {
			t.Errorf("sidebar.js lacks %s", want)
		}
	}
	html := readStaticSrc(t, "index.html")
	if !strings.Contains(html, `<div id="stash-filter-row" class="hidden"><input id="stash-filter"`) || !strings.Contains(html, `id="stash-filter-clear"`) {
		t.Error("index.html: the stash filter row is missing")
	}
	if !strings.Contains(readStaticSrc(t, "style.css"), "#stash-filter-row.hidden") {
		t.Error("style.css: #stash-filter-row.hidden")
	}
}

// The TUI's f (find_current.go) when it cannot land: its words, the web's
// branch filter standing for the / filter.
func TestFindCurrentJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "sidebar.js", "locateMiss") + `
console.log(JSON.stringify([locateMiss("branches", ""), locateMiss("branches", "feat"), locateMiss("worktrees", "")]));
`
	if got := nodeRun(t, src); got != `["no current branch: HEAD is detached","feat is hidden by the branch filter","current worktree unknown"]` {
		t.Errorf("locateMiss = %s", got)
	}
}

func TestFindCurrentWired(t *testing.T) {
	t.Parallel()
	sb := readStaticSrc(t, "sidebar.js")
	for _, want := range []string{
		`title="scroll to the current worktree (f in the TUI)"`,
		`if (n === "worktrees") {`,
		`opLine(locateMiss("branches", head ? head.name : ""), true);`,
		`opLine(locateMiss("worktrees", ""), true);`,
	} {
		if !strings.Contains(sb, want) {
			t.Errorf("sidebar.js lacks %s", want)
		}
	}
}

// The TUI Settings type-to-filter: case-insensitive substring; on the web
// page a row also stays when its heading matches.
func TestSettingsFilterJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "settings.js", "settingsRowMatch") + `
console.log(JSON.stringify([settingsRowMatch("", "commits", "x"), settingsRowMatch("GRAPH", "commits", "show graph lane graph"), settingsRowMatch("refresh", "refresh", "intervals"), settingsRowMatch("hook", "commits", "show graph")]));
`
	if got := nodeRun(t, src); got != `[true,true,true,false]` {
		t.Errorf("settingsRowMatch = %s", got)
	}
}

func TestSettingsFilterWired(t *testing.T) {
	t.Parallel()
	s := readStaticSrc(t, "settings.js")
	for _, want := range []string{
		`<input id="settings-filter" type="text" placeholder="type to filter"`,
		`applySettingsFilter();`,
		`if (state.settingsQuery) {`, // first esc clears the filter, the next closes
		`(no match)`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("settings.js lacks %s", want)
		}
	}
	if !strings.Contains(readStaticSrc(t, "style.css"), "#settings-box .sfhide") {
		t.Error("style.css: #settings-box .sfhide")
	}
}
