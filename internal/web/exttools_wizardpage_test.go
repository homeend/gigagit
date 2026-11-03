package web

import (
	"strings"
	"testing"
)

// The window's model: one entry per tool (rows grouped by purpose, the words a
// reader knows), the configured block's own text, one state per row, and the
// commands no detected tool offers under "Your own commands".
func TestExtToolsModelJS(t *testing.T) {
	t.Parallel()
	var mod strings.Builder
	for _, fn := range []string{"purposeOf", "variantName", "rowKey", "buildTools", "toolCount", "pendingOffers", "keptOffers", "bandOffers", "wizardAddRows", "addStatus"} {
		mod.WriteString(jsFunc(t, "exttools.js", fn) + "\n")
	}
	src := mod.String() + `
const data = {
  wizard: [
    {tool: "claude", label: "Claude Code", category: "session", name: "Claude", configured: true, checked: true, command: "catalog session"},
    {tool: "claude", label: "Claude Code", category: "conflict_complete", name: "Claude — resolve & complete (yolo)", configured: false, checked: false, command: "catalog complete"},
    {tool: "claude", label: "Claude Code", category: "conflict", name: "Claude (yolo, headless)", configured: true, checked: true, command: "catalog conflict"},
    {tool: "claude", label: "Claude Code", category: "review", name: "Claude (interactive)", configured: true, checked: true, command: "catalog review i"},
    {tool: "claude", label: "Claude Code", category: "review", name: "Claude", configured: true, checked: true, command: "catalog review"},
    {tool: "meld", label: "Meld", category: "conflict", name: "Meld", configured: false, checked: true, command: "meld x"},
  ],
  commands: [
    {category: "session", name: "Claude", command: "my session", valid: true, approved: true, mode: "session"},
    {category: "conflict", name: "Claude (yolo, headless)", command: "my conflict", valid: true, approved: false},
    {category: "review", name: "Claude (interactive)", command: "my review i", valid: true, approved: true},
    {category: "review", name: "Claude", command: "my review", valid: false, problem: "bad token", approved: true},
    {category: "commit_message", name: "Team msg", command: "./team.sh", valid: true, approved: true},
  ],
  template_offers: [
    {category: "review", name: "Claude (interactive)", status: "update", reason: "v1 → v2", offer_id: "o1", tool_label: "Claude Code"},
    {category: "session", name: "Claude", status: "update", reason: "v2 → v3", offer_id: "o2", tool_label: "Claude Code", declined: true},
    {category: "conflict", name: "Claude (yolo, headless)", status: "unsupported", reason: "too new"},
    {category: "commit_message", name: "Team msg", status: "update", reason: "v0 → v1", offer_id: "o3", tool_label: "Claude Code"},
  ],
};
const ticks = {};
const tools = buildTools(data, ticks);
const brief = (t) => [t.label, toolCount(t), t.rows.map((r) => [r.purpose, r.variant, r.state, r.command].join("|"))];
const unticked = buildTools(data, {[rowKey("conflict", "Meld")]: false});
console.log(JSON.stringify([
  tools.map(brief),
  tools[0].rows.find((r) => r.name === "Claude (yolo, headless)").why,
  tools[0].rows.find((r) => r.category === "review" && r.name === "Claude").problem,
  tools[0].rows.find((r) => r.category === "conflict").approved,
  [tools[0].rows.find((r) => r.category === "conflict_complete").ticked, tools[1].rows[0].ticked],
  pendingOffers(data).map((o) => o.offer_id),
  keptOffers(data).map((o) => o.offer_id),
  bandOffers(data, "").map((o) => o.offer_id),
  bandOffers(data, "o2").map((o) => o.offer_id),
  variantName("Kimi — resolve & complete"),
  wizardAddRows(tools),
  addStatus({written: 2, path: "/c/gg/config.toml"}),
  addStatus({written: 1, path: "/c/gg/config.toml"}),
  buildTools({wizard: [], commands: [], template_offers: []}, {}).length,
  wizardAddRows(unticked),
]));
`
	want := `[[["Claude Code","4 of 5",[` +
		`"Resolve conflicts|Yolo, headless|unsupported|my conflict",` +
		`"Resolve and finish|Yolo|new|catalog complete",` +
		`"Review changes|Interactive|update|my review i",` +
		`"Review changes|Default|broken|my review",` +
		`"Agent session|Default|kept|my session"]],` +
		`["Meld","0 of 1",["Resolve conflicts|Default|new|meld x"]],` +
		`["Your own commands","1",["Commit message|Team msg|update|./team.sh"]]],` +
		`"too new","bad token",false,[false,true],["o1","o3"],["o2"],["o1","o3"],["o1","o3","o2"],"Default",` +
		`[{"tool":"meld","category":"conflict","name":"Meld"}],` +
		`"2 commands added to /c/gg/config.toml","1 command added to /c/gg/config.toml",0,[]]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("window model =\n%s\nwant\n%s", got, want)
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
		{ext, `const rows = wizardAddRows(tools);`, "adding names the ticked rows by key"},
		{ext, `postJSON("/api/exttools/add", { rows })`, "adding names the ticked rows by key"},
		{ext, `No known tools found on this machine.`, "the empty window"},
		{ext, `new CustomEvent("gg:notices")`, "an answered update leaves the bar's notice count true"},
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
		{ext, "External tools — detected"},
		// A static import moves notifications.js before keys.js in load order,
		// and ! then opens and closes the centre in one press.
		{ext, `./notifications.js`},
		{html, "Add or edit in the TUI settings"},
	} {
		if strings.Contains(stale.src, stale.gone) {
			t.Errorf("stale copy still says %q", stale.gone)
		}
	}
}
