package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// prsendrows.js: the GitHub rows a note's right-click menu offers in a PR's
// own diff — the TUI's forgeNoteRows, row for row.
func TestPRSendRowsJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "prsendrows.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prsendrows.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { sendRows } from "./prsendrows.mjs";
const ids = (rows) => rows.map((r) => r.id + ":" + r.label);
console.log(JSON.stringify({
  local: ids(sendRows({ id: "n1", source: "user", sync: "local", group: "mine" }, 7)),
  failed: ids(sendRows({ id: "n1", source: "user", sync: "failed", group: "mine" }, 7)),
  sending: ids(sendRows({ id: "n1", source: "user", sync: "sending", group: "mine" }, 7)),
  remark: ids(sendRows({ id: "review:r1:0", source: "agent", read_only: true, replyable: true, sync: "local", group: "review:r1" }, 7)),
  thread: ids(sendRows({ id: "forge:C1", source: "forge", read_only: true, replyable: true, sync: "github",
    replies: [{ id: "d1", source: "user" }, { id: "d2", source: "user" }, { id: "forge:C2", source: "forge" }] }, 7)),
  resolved: ids(sendRows({ id: "forge:C3", source: "forge", read_only: true, sync: "github", resolved: true }, 7)),
  reply: ids(sendRows({ id: "d1", parent_id: "forge:C1", source: "user", sync: "local" }, 7)),
  notPR: ids(sendRows({ id: "n1", source: "user", sync: "local", group: "mine" }, 0)),
}));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string][]string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := map[string][]string{
		"local":    {"send:Send as GitHub comment"},
		"failed":   {"send:Retry sending as GitHub comment"},
		"sending":  {},
		"remark":   {"send:Send as GitHub comment"},
		"thread":   {"reply-send:Reply & send…", "resolve:Resolve on GitHub", "send-drafts:Send 2 draft replies"},
		"resolved": {"reply-send:Reply & send…", "resolve:Reopen on GitHub"},
		"reply":    {},
		"notPR":    {},
	}
	for k, w := range want {
		if g := got[k]; !slices.Equal(g, w) && !(len(g) == 0 && len(w) == 0) {
			t.Errorf("%s = %q, want %q", k, g, w)
		}
	}
}

// prsend.js posts only to the send routes, names the PR by its number, and
// is wired into the page (the note menu hook, the module import).
func TestPRSendModuleIsWired(t *testing.T) {
	t.Parallel()
	read := func(f string) string {
		b, err := os.ReadFile(filepath.Join("static", f))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	js := read("prsend.js")
	for _, want := range []string{`"/api/pr/send?n=" + n`, `registerRows("note"`, `registerRows("pr"`} {
		if !strings.Contains(js, want) {
			t.Errorf("prsend.js lacks %q", want)
		}
	}
	if !strings.Contains(read("app.js"), `import "./prsend.js";`) {
		t.Error("app.js does not import prsend.js")
	}
	if !strings.Contains(read("files.js"), `extraRows("note", {`) {
		t.Error("the note menu has no extraRows(\"note\") hook")
	}
	if m := read("menus.js"); !strings.Contains(m, `"note"`) || !strings.Contains(m, `"pr"`) {
		t.Error("menus.js does not know the note / pr menus")
	}
}

// Item 8: the verdict's typed body survives a refusal; the wiring of
// prkept.js (TestPRKeptJS holds the rule: only its own box's changing send
// drops a kept body).
func TestPRSendKeepsTheVerdictBody(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "prsend.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`keptBody = { pr, group: "verdict", text };`,
		`keptBody = keptAfterSend(keptBody, n, body, ev);`,
		`keptText(keptBody, pr, "verdict")`,
		`fn(n, ev, body)`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("prsend.js lacks %q", want)
		}
	}
}

// The panel's pure builders (spec §5.4, the TUI's send_panel.go row for row).
func TestPRSendPanelBuildersJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "prsendrows.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prsendrows.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { panelRows, bodyOptions, panelRequest, tickAllInGroup } from "./prsendrows.mjs";
const cands = { pr: 42, head: "abc", groups: [
  { id: "review:r1", kind: "review", agent: "Claude Code", title: "two nits", created: "2026-10-09T14:02:00Z", slot: 2, rows: [
    { id: "review:r1:1", kind: "remark", severity: "high", path: "internal/git/pull.go", range: [120, 134], side: "new", summary: "lock released twice", rationale: "why", code: ["a", "b"], skip: "", sync: "local" },
    { id: "review:r1:2", kind: "remark", severity: "low", path: "internal/tui/x.go", range: [5, 5], side: "new", summary: "nit", code: [], skip: "its lines changed", sync: "local" } ] },
  { id: "mine", kind: "mine", agent: "", title: "", created: "", slot: 1, rows: [
    { id: "n1", kind: "note", severity: "", path: "README.md", range: [12, 12], side: "new", summary: "typo", code: [], skip: "", sync: "local" } ] },
  { id: "replies", kind: "replies", agent: "", title: "", created: "", slot: 0, rows: [
    { id: "d1", kind: "reply", severity: "", path: "internal/git/pull.go", range: [120, 120], side: "new", summary: "addressed", code: [], skip: "", sync: "local" } ] },
] };
const t0 = new Set(["review:r1:1", "d1", "gone"]);
const rows = panelRows(cands, t0);
const shape = rows.map((r) => r.kind === "group" ? "G:" + r.id + ":" + r.n + ":" + (r.all ? "all" : r.ticked ? "some" : "none") : "R:" + r.id + ":" + r.where + ":" + (r.tickable ? "t" : "x") + (r.ticked ? "+" : "-") + (r.skip ? ":" + r.skip : ""));
const opts = bodyOptions(cands, t0).map((o) => o.value + "=" + o.label);
const req1 = panelRequest(cands, t0, { kind: "review", from: "r1" });
const req2 = panelRequest(cands, t0, { kind: "typed", typed: "hello" });
const req3 = panelRequest(cands, new Set(), { kind: "none" });
const req4 = panelRequest(cands, t0, { kind: "none" });
const all = [...tickAllInGroup(cands, new Set(["d1"]), "review:r1")].sort();
const none = [...tickAllInGroup(cands, new Set(["review:r1:1"]), "review:r1")].sort();
console.log(JSON.stringify({ shape, opts, req1, req2, req3, req4, all, none }));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `{"shape":["G:review:r1:2:all","R:review:r1:1:internal/git/pull.go:120-134:t+","R:review:r1:2:internal/tui/x.go:5:x-:its lines changed","G:mine:1:none","R:n1:README.md:12:t-","G:replies:1:all","R:d1:internal/git/pull.go:120:t+"],` +
		`"opts":["none=no body","review:r1=review text (Claude Code)","typed=typed"],` +
		`"req1":{"kind":"notes","ids":["review:r1:1","d1"],"verdict":true,"body_from":"r1"},` +
		`"req2":{"kind":"notes","ids":["review:r1:1","d1"],"verdict":true,"body":"hello","body_set":true},` +
		`"req3":null,"req4":{"kind":"notes","ids":["review:r1:1","d1"],"verdict":true},` +
		`"all":["d1","review:r1:1"],"none":[]}`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// §5.4 / R7: the PR menu's Send to GitHub… opens the panel; Send review…
// and its helpers are gone; the panel module is imported, reads the
// candidates, posts through sendToGitHub, and keeps its ticks per PR.
func TestPRSendPanelIsWired(t *testing.T) {
	t.Parallel()
	js := readStatic(t, "prsend.js")
	for _, want := range []string{`label: "Verdict…"`, `export async function sendToGitHub(`} {
		if !strings.Contains(js, want) {
			t.Errorf("prsend.js lacks %q", want)
		}
	}
	for _, gone := range []string{`sendReviewPick`, `sendReviewBody`, `groupCount`, `"Send review…"`, `kind: "group"`, `send/groups`} {
		if strings.Contains(js, gone) {
			t.Errorf("prsend.js still has %q", gone)
		}
	}
	p := readStatic(t, "prsendpanel.js")
	for _, want := range []string{`label: "Send to GitHub…"`, `openSendPanel(pr.number)`, `"/api/pr/send/candidates?n=" + n`, `panelRows(`, `bodyOptions(`, `panelRequest(`, `tickAllInGroup(`, `sendToGitHub(`, `pushLayer("prsendpanel"`, `nothing to send to #`, `registerHelp({`, `onSendDone(`, `onHeadMoved(`,
		// W7: an aborted send is a successful no-op (ok, not changed) — the panel and its ticks stay
		`if (!ev.ok || !ev.changed) {`} {
		if !strings.Contains(p, want) {
			t.Errorf("prsendpanel.js lacks %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "app.js"), `import "./prsendpanel.js";`) {
		t.Error("app.js does not import prsendpanel.js")
	}
	if !strings.Contains(readStatic(t, "index.html"), `id="prsendpanel"`) {
		t.Error("index.html has no panel overlay")
	}
	if !strings.Contains(readStatic(t, "style.css"), `#prsendpanel`) {
		t.Error("style.css has no panel rules")
	}
}

// prOfCtx names the PR a note menu's GitHub rows send to: a PR's own diff
// (ctx.preview.pr) or a review view opened from that PR's Reviews block
// (ctx.sendPR) — the latter never rides ctx.preview, whose pr also steers
// the note fetch and the link builder.
func TestPRSendPROfCtxJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "prsendrows.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prsendrows.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { prOfCtx } from "./prsendrows.mjs";
console.log(JSON.stringify({
  prDiff: prOfCtx({ path: "a", preview: { pr: 7 } }),
  review: prOfCtx({ path: "a", review: "r1", sendPR: 7 }),
  plainReview: prOfCtx({ path: "a", review: "r1" }),
  pair: prOfCtx({ path: "a", preview: { pr: 0, pair: { a: "x", b: "y" } } }),
  none: prOfCtx(null),
}));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string]int
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := map[string]int{"prDiff": 7, "review": 7, "plainReview": 0, "pair": 0, "none": 0}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %d, want %d", k, got[k], w)
		}
	}
}

// A review view opened from a PR's Reviews block stamps the PR on its note
// context (sendPR, from state.review.back), and the note menu's GitHub rows
// read it through prOfCtx — the user's "no option to send note as a github
// comment" in a PR review's (stacked) diff.
func TestReviewViewCtxCarriesThePR(t *testing.T) {
	t.Parallel()
	read := func(f string) string {
		b, err := os.ReadFile(filepath.Join("static", f))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if f := read("files.js"); !strings.Contains(f, `sendPR: reviewSendPR()`) || !strings.Contains(f, `back.kind === "pr"`) {
		t.Error("files.js: the review view's note ctx does not carry the PR it was opened from (sendPR)")
	}
	if js := read("prsend.js"); !strings.Contains(js, `import { prOfCtx, sendRows } from "./prsendrows.js";`) || strings.Contains(js, "function prOfCtx") {
		t.Error("prsend.js does not read the PR through prsendrows.js's prOfCtx")
	}
}
