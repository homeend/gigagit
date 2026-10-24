package web

import (
	"encoding/json"
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

// The TUI's ctrl+d cheat sheets (popup_help.go tokensHelp), verbatim, with
// live examples.
func TestTokensHelpJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "tokenhelp.js", "tokensHelp") + `
const now = new Date(Date.UTC(2026, 9, 24, 9, 5, 7));
const show = (ls) => ls.map((l) => (l.heading ? "# " : "") + l.text).join("\n");
console.log(JSON.stringify([show(tokensHelp(now, false)), show(tokensHelp(now, true))]));
`
	tok := func(k, d string) string { return k + strings.Repeat(" ", 22-len([]rune(k))) + d }
	tail := []string{"", "# Date format (FMT)",
		tok("yyyy", "year, 4 digits"), tok("MM", "month 01–12"), tok("dd", "day 01–31"),
		tok("HH", "hour 00–23"), tok("mm", "minute 00–59"), tok("ss", "second 00–59"),
		"Other characters pass through as separators (avoid yyyy/MM/dd/HH/mm/ss",
		"and digits inside literal text — they are format verbs, not literals).",
		"", "# Examples",
		tok("<date>", "2026-10-24"), tok("<date:yyyy-MM-dd>", "2026-10-24"), tok("<date:yyyyMMdd-HHmm>", "20261024-0905")}
	prefix := append([]string{"# Tokens",
		tok("<user:LABEL>", "asks you for LABEL whenever the prefix is used"),
		tok("<seq:NAME>", "per-repo counter NAME (1, 2, …)"),
		tok("<seq:NAME:N>", "the same, zero-padded to N digits"),
		tok("<date>", "today as yyyy-MM-dd"),
		tok("<date:FMT>", "now, formatted by FMT (see below)"),
		tok("<parent-branch>", "the branch the new branch forks from"),
		tok("<repo>", "the repository directory name"),
		tok("<random-alpha:N>", "N random lowercase letters"),
		tok("<random-num:N>", "N random digits")}, tail...)
	text := append([]string{"# Tokens",
		tok("<user:LABEL>", "asks you for LABEL whenever the text is used"),
		tok("<seq:NAME>", "per-repo counter NAME (1, 2, …)"),
		tok("<seq:NAME:N>", "the same, zero-padded to N digits"),
		tok("<date>", "today as yyyy-MM-dd"),
		tok("<date:FMT>", "now, formatted by FMT (see below)"),
		tok("<branch>", "the branch you are on"),
		tok("<parent-branch>", "the same as <branch> here"),
		tok("<repo>", "the repository directory name"),
		tok("<random-alpha:N>", "N random lowercase letters"),
		tok("<random-num:N>", "N random digits"),
		"Any other <…> stays in the text as written."}, tail...)
	var got []string
	if err := json.Unmarshal([]byte(nodeRun(t, src)), &got); err != nil {
		t.Fatal(err)
	}
	if got[0] != strings.Join(prefix, "\n") {
		t.Errorf("prefix sheet =\n%s\nwant\n%s", got[0], strings.Join(prefix, "\n"))
	}
	if got[1] != strings.Join(text, "\n") {
		t.Errorf("text sheet =\n%s\nwant\n%s", got[1], strings.Join(text, "\n"))
	}
}

func TestTokensHelpWired(t *testing.T) {
	t.Parallel()
	p := readStaticSrc(t, "prefixes.js")
	for _, want := range []string{`openTokensHelp(false)`, `data-act="formats"`, `ctrl+d formats`} {
		if !strings.Contains(p, want) {
			t.Errorf("prefixes.js lacks %s", want)
		}
	}
	tt := readStaticSrc(t, "texttemplates.js")
	for _, want := range []string{`openTokensHelp(true)`, `data-act="formats"`, `ctrl+d formats`} {
		if !strings.Contains(tt, want) {
			t.Errorf("texttemplates.js lacks %s", want)
		}
	}
	th := readStaticSrc(t, "tokenhelp.js")
	for _, want := range []string{`"Prefix tokens & date formats"`, `"Text template tokens & date formats"`} {
		if !strings.Contains(th, want) {
			t.Errorf("tokenhelp.js lacks the title %s", want)
		}
	}
	if !strings.Contains(readStaticSrc(t, "console.js"), "if (textTemplatesKey(e)) return true; // alt+x reaches gg past an unfocused console") {
		t.Error("console.js: alt+x must open text templates over an unfocused console")
	}
}

// The TUI's List notes… rows (notes_list_popup.go): one per thread.
func TestNoteListJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "notelist.js", "noteListEntries") + "\n" + jsFunc(t, "notelist.js", "noteListLine") + "\n" + jsFunc(t, "notelist.js", "noteListVisible") + `
const ns = [
  { id: "a", source: "user", author: "ada", side: "new", line: 15, range: [15, 15], summary: "check the bound", status: "ok", replies: [{ id: "a1" }, { id: "a2" }] },
  { id: "b", source: "agent", author: "", side: "old", line: 15, range: [15, 23], summary: "moved", status: "stale" },
  { id: "c", source: "forge", author: "bob", side: "new", line: 0, range: [0, 0], summary: "whole file", status: "ok", file_level: true, resolved: true },
];
const es = noteListEntries(ns, "");
const st = noteListEntries(ns.slice(0, 1), "src/a.go");
console.log(JSON.stringify([es.map(noteListLine), st.map(noteListLine), es.map((e) => [e.stale, e.agent]), noteListVisible(es, "BOB").map((e) => e.rootId), noteListVisible(st, "a.go").length, noteListVisible(es, "").length]));
`
	want := `[["◆ new:15  ada  check the bound  +2","◆ old:15-23  moved","◆ file  review  bob  whole file  resolved"],["◆ src/a.go new:15  ada  check the bound  +2"],[[false,false],[true,true],[false,false]],["c"],1,3]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("note list =\n%s\nwant\n%s", got, want)
	}
}

func TestNoteListWired(t *testing.T) {
	t.Parallel()
	nl := readStaticSrc(t, "notelist.js")
	for _, want := range []string{`"Notes on " + title`, `(no matching notes)`, `[↑/↓] move  [enter] go to note  type to filter  [esc] close`, `Note is no longer in this diff`, `landNote(`} {
		if !strings.Contains(nl, want) {
			t.Errorf("notelist.js lacks %s", want)
		}
	}
	f := readStaticSrc(t, "files.js")
	if strings.Count(f, `{ label: "List notes…", act: () => openNotesList() }`) < 2 {
		t.Error("files.js: List notes… belongs on the diff's menu and on a ◆ row's")
	}
}
