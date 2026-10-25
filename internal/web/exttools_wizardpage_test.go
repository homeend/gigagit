package web

import (
	"strings"
	"testing"
)

// The wizard's rows read as the TUI's, and a write names only the checked,
// not-yet-configured rows by key.
func TestExtToolsWizardJS(t *testing.T) {
	t.Parallel()
	var mod strings.Builder
	for _, fn := range []string{"wizardRowText", "wizardAddRows", "wizardStatus"} {
		mod.WriteString(jsFunc(t, "exttools.js", fn) + "\n")
	}
	src := mod.String() + `
const rows = [
  {tool: "claude", label: "Claude Code", category: "review", name: "Claude", configured: true},
  {tool: "claude", label: "Claude Code", category: "review", name: "Claude (i)", configured: true, status: "update", offer_id: "o1"},
  {tool: "claude", label: "Claude Code", category: "review", name: "Claude (d)", configured: true, status: "update", offer_id: "o2", declined: true},
  {tool: "claude", label: "Claude Code", category: "conflict", name: "Claude (u)", configured: true, status: "unsupported"},
  {tool: "meld", label: "Meld", category: "conflict", name: "Meld"},
  {tool: "claude", label: "Claude Code", category: "conflict", name: "Claude (yolo)"},
];
const checked = [true, true, true, false, true, false];
console.log(JSON.stringify([
  rows.map((r, i) => wizardRowText(r, checked[i], i === 4)),
  wizardAddRows(rows, checked),
  wizardStatus({written: 2, path: "/c/gg/config.toml"}),
  wizardStatus({written: 0, path: "/c/gg/config.toml"}),
  wizardStatus(null, "disk full"),
]));
`
	want := `[["  [x] Claude Code — review: Claude (configured)",` +
		`"  [x] Claude Code — review: Claude (i) (update available — u)",` +
		`"  [x] Claude Code — review: Claude (d) (configured)",` +
		`"  [ ] Claude Code — conflict: Claude (u) (agent version unsupported)",` +
		`"> [x] Meld — conflict: Meld",` +
		`"  [ ] Claude Code — conflict: Claude (yolo)"],` +
		`[{"tool":"meld","category":"conflict","name":"Meld"}],` +
		`"external tools: 2 command(s) written to /c/gg/config.toml",` +
		`"external tools: nothing to write (already configured or unchecked)",` +
		`"external tools: disk full"]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("wizard model =\n%s\nwant\n%s", got, want)
	}
}

// The location popup's rows and actions read as the TUI's.
func TestRepoLocJS(t *testing.T) {
	t.Parallel()
	var mod strings.Builder
	for _, fn := range []string{"repoLocLabel", "repoLocSlot"} {
		mod.WriteString(jsFunc(t, "repoloc.js", fn) + "\n")
	}
	src := mod.String() + `
console.log(JSON.stringify([
  ["copy-to-private", "move-to-private", "copy-to-committed", "move-to-committed", "x"].map(repoLocLabel),
  repoLocSlot({path: "/r/.gg.toml", exists: true}),
  repoLocSlot({path: "/p/config.toml", exists: false}),
  repoLocSlot({path: "", exists: false}),
]));
`
	want := `[["Copy to private (user dir)","Move to private (user dir)","Copy to committed (.gg.toml)","Move to committed (.gg.toml)","?"],` +
		`"present  /r/.gg.toml","absent   /p/config.toml","(unavailable)"]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("location model =\n%s\nwant\n%s", got, want)
	}
}

func TestToolsWizardWired(t *testing.T) {
	t.Parallel()
	ext := readStaticSrc(t, "exttools.js")
	loc := readStaticSrc(t, "repoloc.js")
	set := readStaticSrc(t, "settings.js")
	html := readStaticSrc(t, "index.html")
	css := readStaticSrc(t, "style.css")
	for _, pin := range []struct{ src, want, why string }{
		{ext, `postJSON("/api/exttools/add", { rows: wizardAddRows(`, "enter writes the checked rows by key"},
		{ext, `External tools — detected`, "the TUI's wizard title"},
		{ext, `no known tools detected on this machine (looked for: claude, junie, meld)`, "the TUI's empty line"},
		{ext, `already configured — skipped on apply`, "the configured row's detail"},
		{ext, `writes to: `, "a new row's detail"},
		{ext, `[space] toggle  [u] review update  [enter] write to global config  [esc] back`, "the TUI's hint"},
		{ext, `opLine(wizardStatus(`, "the status line"},
		{loc, `postJSON("/api/settings/repo-location", { action, overwrite })`, "an action with the overwrite answer"},
		{loc, `active file = private if present; move deletes the source (may dirty a tracked .gg.toml)`, "the TUI's hint"},
		{loc, `(nothing to move — no per-repo config here, or not in a repo)`, "the TUI's empty line"},
		{loc, `[y] overwrite  [n/esc] cancel`, "the confirm keys"},
		{loc, `[↑/↓] select  [enter] do  [esc] close`, "the list keys"},
		{set, `data-act="exttools"`, "Settings opens the wizard (the TUI's first Settings row)"},
		{set, `data-act="repoloc"`, "Settings opens the location popup"},
		{html, `<div id="repoloc" class="hidden">`, "the location overlay"},
		{css, `#repoloc.hidden`, "its own hidden rule"},
	} {
		if !strings.Contains(pin.src, pin.want) {
			t.Errorf("%s: missing %q", pin.why, pin.want)
		}
	}
	for _, stale := range []struct{ src, gone string }{
		{set, "move it from the TUI"},
		{set, "the tools wizard and language: in the TUI settings"},
		{ext, "add or edit in the TUI settings"},
		{html, "Add or edit in the TUI settings"},
	} {
		if strings.Contains(stale.src, stale.gone) {
			t.Errorf("stale copy still says %q", stale.gone)
		}
	}
}
