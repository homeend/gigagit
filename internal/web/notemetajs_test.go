package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// notemeta.js is import-free so node can pin it against the wire shape the
// server sends (notemeta.Resolved / notemeta.Counts JSON).
func TestNotemetaJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join("static", "notemeta.js"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notemeta.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import * as M from "./notemeta.mjs";
const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
const elide = (s, n) => s.length <= n ? s : n <= 1 ? "…" : "…" + s.slice(s.length - n + 1);
const d = { badges: [
  { key: "kind", value: "issue", color: "red", style: "filled" },
  { key: "severity", value: "high", color: "orange", style: "text", glyph: "!" },
  { key: "status", value: "disagreed", color: "red", style: "text" },
  { key: "x", value: "<b>", color: "crimson", style: "text" } ],
  audience: "author", frame: "red",
  lines: [{ label: "confidence", value: "high" }],
  stored: [{ key: "tool", value: "lint" }],
  added: [{ key: "status", from: "agreed", to: "disagreed", by: "MR" }, { key: "k", to: "v" }],
  problems: ["display.kind.values.issue.color: \"crimson\" is not a colour"] };
const out = {
  run: M.badgeRunHTML(d, esc),
  runCols: M.badgeRunCols(d),
  title: M.titleMetaHTML(d, esc),
  none: M.titleMetaHTML(undefined, esc) + "|" + M.badgeRunHTML({}, esc) + "|" + M.metaLinesHTML(null, esc),
  lines: M.metaLinesHTML(d, esc),
  details: M.metaDetailsHTML(d, esc),
  frame: [M.frameClass(d), M.frameClass({ frame: "crimson" }), M.frameClass(null)],
  aud: [M.audienceWord("author"), M.audienceWord("reviewer"), M.audienceWord("x")],
  counts: M.countsText({ groups: [{ key: "kind", label: "kind", values: [{ value: "issue", color: "red", n: 2 }, { value: "good", n: 1 }] }], author: 1, reviewer: 3 }),
  countsRun: M.countsRunHTML({ groups: [{ key: "kind", values: [{ value: "issue", color: "red", n: 2 }] }] }, esc),
  countsNone: M.countsText(null) + "|" + M.countsRunHTML({}, esc),
  sev: [M.severityColour(d, "high"), M.severityColour(d, "low"), M.severityColour(null, "high")],
  fitWhole: M.fitNoteTitle(10, "note · bot · ", "a/b/c.go R12", 60, elide),
  fitPath: M.fitNoteTitle(10, "note · bot · ", "internal/deep/dir/file.go R12", 40, elide),
  fitWords: M.fitNoteTitle(10, "note · somebody long · ", "internal/deep/dir/file.go R12", 28, elide),
  fitTiny: M.fitNoteTitle(10, "note · bot · ", "internal/deep/dir/file.go R12", 14, elide),
  fitNoRoom: M.fitNoteTitle(10, "note · bot · ", "a.go R1", 0, elide),
};
console.log(JSON.stringify(out));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, raw)
	}
	var got struct {
		Run, Title, None, Lines, Details, Counts, CountsRun, CountsNone string
		RunCols                                                         int
		Frame, Aud, Sev                                                 []string
		FitWhole, FitPath, FitWords, FitTiny, FitNoRoom                 struct{ Words, At string }
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	want := `<span class="mchip mc-red ms-filled"> issue </span> ` +
		`<span class="mchip mc-orange ms-text">! high</span>  ` +
		`<span class="mchip mc-red ms-text">disagreed</span>  ` +
		`<span class="mchip mc-grey ms-text">&lt;b&gt;</span>  ` +
		`<span class="maud">→ author</span>`
	if got.Run != want {
		t.Errorf("run =\n%s\nwant\n%s", got.Run, want)
	}
	// " issue " 7 + 1 + "! high" 6 + 2 + "disagreed" 9 + 2 + "<b>" 3 + 2 + "→ author" 8
	if got.RunCols != 40 {
		t.Errorf("runCols = %d, want 40", got.RunCols)
	}
	if got.Title != want+`<span class="msep"> · </span>` {
		t.Errorf("title = %s", got.Title)
	}
	if got.None != "||" {
		t.Errorf("no display must draw nothing: %q", got.None)
	}
	if got.Lines != `<div class="mline">confidence: high</div>` {
		t.Errorf("lines = %s", got.Lines)
	}
	wantD := `<div class="mdetail">meta: tool: lint</div>` +
		`<div class="mdetail">added: status: agreed → disagreed (MR) · k: v</div>` +
		`<div class="mdetail">display problem: display.kind.values.issue.color: &quot;crimson&quot; is not a colour</div>`
	if got.Details != wantD {
		t.Errorf("details =\n%s\nwant\n%s", got.Details, wantD)
	}
	if got.Frame[0] != "mf-red" || got.Frame[1] != "" || got.Frame[2] != "" {
		t.Errorf("frame = %v (an unknown colour has no class)", got.Frame)
	}
	if got.Aud[0] != "→ author" || got.Aud[1] != "for you" || got.Aud[2] != "" {
		t.Errorf("aud = %v", got.Aud)
	}
	if got.Counts != "2 issue · 1 good · 1 for author · 3 for you" {
		t.Errorf("counts = %q", got.Counts)
	}
	if got.CountsRun != `issue <span class="mc-red ms-text">2</span>` || got.CountsNone != "|" {
		t.Errorf("countsRun = %q none = %q", got.CountsRun, got.CountsNone)
	}
	if got.Sev[0] != "orange" || got.Sev[1] != "" || got.Sev[2] != "" {
		t.Errorf("sev = %v", got.Sev)
	}
	// The TUI's fit: whole when it fits; the path elided (≥8) first; then the
	// words before it; then the path's tail; badges never.
	if got.FitWhole.Words != "note · bot · " || got.FitWhole.At != "a/b/c.go R12" {
		t.Errorf("fitWhole = %+v", got.FitWhole)
	}
	if got.FitPath.Words != "note · bot · " || got.FitPath.At != "…/dir/file.go R12" {
		t.Errorf("fitPath = %+v (40-10 badge cols = 30 room; words 13 → path 17)", got.FitPath)
	}
	// words 23 + path ≥ 8 > 18: the path stops at 8, the words take the rest.
	if got.FitWords.At != "….go R12" || got.FitWords.Words != "note · so…" {
		t.Errorf("fitWords = %+v", got.FitWords)
	}
	if got.FitTiny.Words != "" || got.FitTiny.At != "…R12" {
		t.Errorf("fitTiny = %+v", got.FitTiny)
	}
	if got.FitNoRoom.Words != "note · bot · " || got.FitNoRoom.At != "a.go R1" {
		t.Errorf("room 0 = unknown width: render whole, got %+v", got.FitNoRoom)
	}
}
