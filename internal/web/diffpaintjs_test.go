package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pureJS runs one guarded pure section of a static file under node and
// returns what expr (evaluated after the section) prints as JSON.
func pureJS(t *testing.T, file, start, end, expr string, out any) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", file))
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(string(src), start), strings.Index(string(src), end)
	if i < 0 || j < i {
		t.Fatalf("%s: the guarded section markers are gone (%q / %q)", file, start, end)
	}
	dir := t.TempDir()
	purePath := filepath.Join(dir, "pure.js")
	scriptPath := filepath.Join(dir, "check.mjs")
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
console.log(JSON.stringify(new Function(pure + "; return (" + process.argv[3] + ");")()));
`
	if err := os.WriteFile(purePath, src[i:j], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command(node, scriptPath, purePath, expr).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("node output %q: %v", raw, err)
	}
}

// The chunked painter covers every row exactly once, in order: the first
// (synchronous) range, then fixed-size ranges to the end.
func TestChunkRangesJS(t *testing.T) {
	t.Parallel()
	var got [][][2]int
	pureJS(t, "diffpaint.js", "// --- chunk ranges (pure; guarded against Go) ---", "// --- end chunk ranges ---",
		`[chunkRanges(0, 50, 400), chunkRanges(30, 50, 400), chunkRanges(1000, 50, 400), chunkRanges(450, 50, 400)]`, &got)
	want := [][][2]int{
		{},
		{{0, 30}},
		{{0, 50}, {50, 450}, {450, 850}, {850, 1000}},
		{{0, 50}, {50, 450}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for n := range want {
		if len(got[n]) != len(want[n]) {
			t.Fatalf("case %d: got %v want %v", n, got[n], want[n])
		}
		for k := range want[n] {
			if got[n][k] != want[n][k] {
				t.Fatalf("case %d: got %v want %v", n, got[n], want[n])
			}
		}
	}
}

// Every reader of a painted diff's rows must go through the accessor that
// finishes the paint first (diffBody / histDiff), and only code that never
// reads rows may take the raw element (diffBodyEl / histDiffEl). Naming the
// element any other way bypasses that choice, so it is allowed only in the
// accessors themselves.
func TestDiffBodyReadsGoThroughAccessor(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("static", "*.js"))
	if err != nil {
		t.Fatal(err)
	}
	bad := regexp.MustCompile(`\$\("(diff-body|history-diff)"\)|getElementById\("(diff-body|history-diff)"\)|["'` + "`" + `]#(diff-body|history-diff)\b`)
	allowed := regexp.MustCompile(`^\s*(const|function) (diffBodyEl|histDiffEl)\b|^\s*return \$\("(diff-body|history-diff)"\); // the accessor$`)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") || !bad.MatchString(line) || allowed.MatchString(line) {
				continue
			}
			t.Errorf("%s:%d names the diff host directly — use diffBodyEl()/diffBody() (histDiffEl()/histDiff()): %s", filepath.Base(f), n+1, strings.TrimSpace(line))
		}
	}
}

// Both hosts paint through the chunked painter: a host left on one-shot
// innerHTML would pay the whole table's layout on every open.
func TestDiffHostsPaintInChunks(t *testing.T) {
	t.Parallel()
	if !strings.Contains(jsFunc(t, "files.js", "renderDiff"), "paintTable(diffBodyEl(), t,") {
		t.Error("files.js: renderDiff does not paint through paintTable")
	}
	if !strings.Contains(jsFunc(t, "filehist.js", "renderHistoryDiff"), "paintTable(host, hist.table,") {
		t.Error("filehist.js: renderHistoryDiff does not paint through paintTable")
	}
}

// End lands on the file's end, not the end of what is painted so far; and
// the first change opens centred, so the rows below it are painted first.
func TestDiffLandingsPaintEnough(t *testing.T) {
	t.Parallel()
	if !strings.Contains(jsFunc(t, "files.js", "diffScrollKey"), `if (e.key === "End") finishPaint(diffBodyEl());`) {
		t.Error("files.js: End in the diff pane must finish the paint before jumping to the bottom")
	}
	if !strings.Contains(jsFunc(t, "files.js", "jumpToFirstChange"), "half a screen below it") {
		t.Error("files.js: jumpToFirstChange must paint rows below the first change so it can centre")
	}
}

// A big diff is painted as a run of tables the browser may skip while they
// are off screen (content-visibility): appending one costs that chunk, not a
// re-layout of the whole file — the choppiness of the first seconds.
func TestDiffChunksAreSkippableTables(t *testing.T) {
	t.Parallel()
	src := readStatic(t, "diffpaint.js")
	for _, want := range []string{
		"content-visibility:auto",
		"contain-intrinsic-size:auto ",
		`job.last.insertAdjacentHTML("afterend", chunkOpen(job.t.open,`,
		"3 * Math.max(scroller.clientHeight, 300)", // the first paint covers three screens
	} {
		if !strings.Contains(src, want) {
			t.Errorf("diffpaint.js: missing %q", want)
		}
	}
}

// Rows of one diff now span several tables: a walk from row to row must
// cross into the next table, or a range band / a change block stops at a
// chunk's edge.
func TestRowWalksCrossChunkTables(t *testing.T) {
	t.Parallel()
	for _, fn := range []string{"renderedRows", "firstNewSideRow"} {
		body := jsFunc(t, "files.js", fn)
		if strings.Contains(body, ".nextElementSibling") || !strings.Contains(body, "nextRow(") {
			t.Errorf("files.js: %s must walk with nextRow (crosses chunk tables), not nextElementSibling", fn)
		}
	}
	var got []string
	pureJS(t, "files.js", "// --- next row (pure; guarded against Go) ---", "// --- end next row ---", `(() => {
  // two chunk tables: rows a,b | c — and a stray element after them
  const mk = (id) => ({ id, nextElementSibling: null });
  const a = mk("a"), b = mk("b"), c = mk("c");
  a.nextElementSibling = b;
  const t2 = { matches: (s) => s === "table.diff", querySelector: () => c, nextElementSibling: { matches: () => false } };
  const t1 = { matches: (s) => s === "table.diff", nextElementSibling: t2 };
  for (const r of [a, b]) r.closest = () => t1;
  c.closest = () => t2;
  const out = [];
  for (let r = a; r; r = nextRow(r)) out.push(r.id);
  return out;
})()`, &got)
	if strings.Join(got, "") != "abc" {
		t.Fatalf("walk = %v; want a b c across the two tables", got)
	}
}

// A re-render of the same diff (f, w, the file list hidden, an unfold, a
// note-title refit) paints in chunks like an open: finding the reader's
// change block reads only the rows already painted (the reader cannot be
// past them), landing back on it paints only down to it, and a note title
// measures the host's width without finishing the table. Each of these
// once finished the whole paint: ~1.5 s blocked on a 13k-row file.
func TestRerenderKeepsChunkedPaint(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ fn, bad string }{
		{"noteTitleRoom", "diffBody()"},
		{"visibleChangeBlock", "diffChangeBlocks()"},
		{"rerenderDiffKeepingPlace", "diffChangeBlocks()"},
		{"rerenderDiffKeepingPlace", "diffBody()"},
	} {
		if strings.Contains(jsFunc(t, "files.js", c.fn), c.bad) {
			t.Errorf("files.js: %s calls %s, which finishes the whole paint", c.fn, c.bad)
		}
	}
}

// The re-render's landing paints half a screen past its block, as the first
// change's landing does, so scrollIntoView can centre it.
func TestPaintToBlockLeavesRoomBelow(t *testing.T) {
	t.Parallel()
	if !strings.Contains(jsFunc(t, "files.js", "paintToBlock"), "half") {
		t.Error("files.js: paintToBlock stops at its block's chunk — a block near the chunk's end lands at the bottom of the pane")
	}
}
