package web

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
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

// The TUI's ◇ Staged pseudo-row (wip_rows.go) under ◇ Working tree.
func TestStagedRowJS(t *testing.T) {
	t.Parallel()
	src := "let state = {};\n" + jsFunc(t, "status.js", "stagedRow") + "\n" + jsFunc(t, "status.js", "wtCount") + "\n" + jsFunc(t, "status.js", "pseudoKind") + "\n" +
		jsFunc(t, "commits.js", "sideSpec") + "\n" + jsFunc(t, "commits.js", "markedCompareEnds") + `
const out = [];
for (const wt of [null, { counts: { unstaged: 1 } }, { counts: { staged: 2, unstaged: 1 } }, { counts: { staged: 1 } }]) {
  state = { wt };
  out.push([wtCount(), [0, 1, 2].map(pseudoKind)]);
}
out.push(["@worktree", "@staged", "abc123"].map(sideSpec));
out.push(markedCompareEnds([], ["@worktree", "@staged"]));
out.push(markedCompareEnds([], ["@staged", "abc"]).tip);
out.push(markedCompareEnds([], ["@staged", "abc", "def"]).err);
console.log(JSON.stringify(out));
`
	want := `[[0,["","",""]],[1,["wt","",""]],[2,["wt","staged",""]],[2,["wt","staged",""]],["worktree","staged","commit:abc123"],{"base":"@staged","tip":"@worktree","oldest":"@staged","newest":"@worktree"},"@staged","range compare (3+) is commits-only; remove the working tree / staged row"]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("staged row =\n%s\nwant\n%s", got, want)
	}
}

func TestStagedRowWired(t *testing.T) {
	t.Parallel()
	c := readStaticSrc(t, "commits.js")
	for _, want := range []string{
		`const STAGED_MARK = "@staged";`,
		`pk === "staged" ? stagedRowHTML(i)`,
		`if (pseudoKind(i) === "staged") return openStagedCompare();`,
		`openSidesCompare(sideSpec(ends.base), sideSpec(ends.tip))`,
		`"mark the staged changes (ctrl+click)"`,
		`if (state.cmarks.has(STAGED_MARK) && stagedRow()) out.push(STAGED_MARK);`,
	} {
		if !strings.Contains(c, want) {
			t.Errorf("commits.js lacks %s", want)
		}
	}
	if strings.Contains(c, "state.wt && i === 0") || strings.Contains(c, "state.wt && state.cursor === 0") {
		t.Error("commits.js: pseudo-row checks go through pseudoKind (there are two rows now)")
	}
	if !strings.Contains(readStaticSrc(t, "status.js"), `<span class="subj">Staged</span>`) {
		t.Error("status.js: the Staged row")
	}
}

// ◇ Staged compares HEAD ↔ index LIVE: "head" resolves at each request, so
// a refresh after a commit compares against the new HEAD (the TUI's
// wipEndpoints), never the one the view opened with.
func TestCompareSidesLiveHead(t *testing.T) {
	t.Parallel()
	dir := editRepo(t, 2)
	writeFile(t, dir, "s.txt", "staged\n")
	gitRun(t, dir, "add", "s.txt")
	ts := serve(t, New(domain.Open(dir)))
	var a sidesResp
	if code := getJSON(t, ts, "/api/compare-sides?left=head&right=staged", &a); code != http.StatusOK {
		t.Fatalf("head vs staged = %d", code)
	}
	if len(a.Files) != 1 || a.Files[0].Path != "s.txt" || a.Left.Spec != "head" {
		t.Fatalf("before the commit = %+v left %+v", a.Files, a.Left)
	}
	gitRun(t, dir, "commit", "-qm", "c3")
	var b sidesResp
	if code := getJSON(t, ts, "/api/compare-sides?left=head&right=staged", &b); code != http.StatusOK {
		t.Fatalf("after commit = %d", code)
	}
	if len(b.Files) != 0 {
		t.Errorf("after the commit = %+v, want nothing staged against the new HEAD", b.Files)
	}
}

// Review fixes: the cursor follows its row when a pseudo-row appears or goes.
func TestPseudoRowCursorShiftJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "status.js", "shiftCursor") + `
console.log(JSON.stringify([shiftCursor(5, 1, 2, false), shiftCursor(5, 2, 1, false), shiftCursor(1, 2, 1, true), shiftCursor(0, 1, 2, false), shiftCursor(3, 0, 1, false), shiftCursor(2, 2, 2, false)]));
`
	if got := nodeRun(t, src); got != `[6,4,0,0,4,2]` {
		t.Errorf("shiftCursor = %s", got)
	}
}

func TestF4ReviewFixesWired(t *testing.T) {
	t.Parallel()
	if !strings.Contains(readStaticSrc(t, "status.js"), "state.cursor = shiftCursor(state.cursor, before, wtCount(), wasStaged);") {
		t.Error("status.js: applyStatus must keep the cursor on its row")
	}
	if !strings.Contains(readStaticSrc(t, "commits.js"), `openSidesCompare("head", "staged")`) {
		t.Error("commits.js: ◇ Staged compares against the live HEAD")
	}
	nl := readStaticSrc(t, "notelist.js")
	for _, want := range []string{"if (e.slot != null && state.stack) await readyStackSlot(state.stack, e.slot);", "if (e.agent && state.notesAgentOff) toggleNotesAgent();"} {
		if !strings.Contains(nl, want) {
			t.Errorf("notelist.js lacks %s", want)
		}
	}
	if !strings.Contains(readStaticSrc(t, "settings.js"), "/^(INPUT|TEXTAREA|SELECT|BUTTON)$/") {
		t.Error("settings.js: Space on a focused button must stay the button's")
	}
}
