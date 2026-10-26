package web

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// mergeToks lays a tok=1 answer's runs onto the diff's rows by source line
// number and reports which rows changed — only those cells are repainted.
func TestMergeToksJS(t *testing.T) {
	t.Parallel()
	var got struct {
		Changed []int            `json:"changed"`
		Rows    []map[string]any `json:"rows"`
	}
	pureJS(t, "difftok.js", "// --- merge toks (pure; guarded against Go) ---", "// --- end merge toks ---", `(() => {
  const rows = [{ left_no: 1, right_no: 1 }, { left_no: 0, right_no: 2 }, { left_no: 2, right_no: 0 }];
  const changed = mergeToks(rows, { old: [[[0, 3, "kw"]], null], new: [null, [[1, 2, "str"]]] });
  return { changed, rows };
})()`, &got)
	if !reflect.DeepEqual(got.Changed, []int{0, 1}) {
		t.Fatalf("changed = %v; want [0 1]", got.Changed)
	}
	if got.Rows[0]["left_tok"] == nil || got.Rows[0]["right_tok"] != nil {
		t.Fatalf("row 0 = %v; want left runs only (new line 1 has none)", got.Rows[0])
	}
	if got.Rows[1]["right_tok"] == nil || got.Rows[2]["left_tok"] != nil {
		t.Fatalf("rows 1/2 = %v / %v", got.Rows[1], got.Rows[2])
	}
}

// The single-file pane's opens and the history overlay ask for the rows
// first; the stack and the quiet refresh after a staging action keep the
// one-request diff.
func TestGetDiffDeferPinned(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	for _, want := range []string{
		`getDiff("/api/entry-diff?" + q, { deferTok: true })`,
		`getDiff("/api/diff?" + q, { deferTok: true })`,
		`getDiff(fileDiffURL(f), { deferTok: true }), fetchNotes(false)]);`,
	} {
		if !strings.Contains(files, want) {
			t.Errorf("files.js: missing %q", want)
		}
	}
	if n := strings.Count(files, `getDiff(fileDiffURL(f), { deferTok: true }), fetchNotes(false)]);`); n != 2 {
		t.Errorf("files.js: openFile and openStatusDiff both defer; found %d", n)
	}
	if !strings.Contains(files, "d = await getDiff(fileDiffURL(f));") {
		t.Error("files.js: quietRefreshFile must keep the one-request diff (it may land in a stack slot)")
	}
	if strings.Contains(readStatic(t, "stackview.js"), "deferTok") {
		t.Error("stackview.js must not defer: a stack never fetches the second half")
	}
	if !strings.Contains(readStatic(t, "filehist.js"), `getDiff("/api/diff?" + q, { deferTok: true })`) {
		t.Error("filehist.js: the history overlay must ask for the rows first")
	}
}

// A colour fetch left behind is aborted: the next revision and closing the
// overlay both cancel it, so the server stops lexing what nobody will see.
func TestHistoryAbortsStaleTok(t *testing.T) {
	t.Parallel()
	hist := readStatic(t, "filehist.js")
	for _, fn := range []string{"closeHistory", "openHistoryDiff"} {
		if !strings.Contains(jsFunc(t, "filehist.js", fn), "abortHistTok()") {
			t.Errorf("filehist.js: %s does not abort the pending colour fetch", fn)
		}
	}
	if !strings.Contains(hist, "fetchToks(") {
		t.Error("filehist.js never fetches the deferred colours")
	}
	if !strings.Contains(jsFunc(t, "files.js", "renderDiff"), "fetchToks(") {
		t.Error("files.js: renderDiff never fetches the deferred colours")
	}
}

// Whatever overwrites the diff body ends the diff that was there, so it
// cancels that diff's colour fetch: otherwise the server lexes a big file
// for a second nobody will see (renderDiff, the one painter, cancels its
// own predecessor).
func TestDiffBodyWritersCancelColours(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("static", "*.js"))
	if err != nil {
		t.Fatal(err)
	}
	fn := regexp.MustCompile(`(?m)^(?:async )?function (\w+)\(`)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(src)
		starts := fn.FindAllStringSubmatchIndex(s, -1)
		for k, m := range starts {
			end := len(s)
			if k+1 < len(starts) {
				end = starts[k+1][0]
			}
			body, name := s[m[0]:end], s[m[2]:m[3]]
			if !strings.Contains(body, "diffBodyEl().innerHTML =") && !strings.Contains(body, "state.lastDiff = null") {
				continue
			}
			if !strings.Contains(body, "abortDiffTok()") {
				t.Errorf("%s: %s replaces the diff on screen without abortDiffTok()", filepath.Base(f), name)
			}
		}
	}
}

// Late colours hold back only the cells holding an end of the reader's
// selection (rewriting one would move that end): a selection left standing
// must not keep the whole file plain, and one reaching outside the diff
// must not be cut where it enters it.
func TestPatchCellsHoldsOnlySelectionEnds(t *testing.T) {
	t.Parallel()
	src := readStatic(t, "difftok.js")
	if strings.Contains(src, "commonAncestorContainer") {
		t.Error("difftok.js: patchCells gates on the selection's common ancestor — the whole host waits, and a selection reaching outside it is missed")
	}
	if !strings.Contains(jsFunc(t, "difftok.js", "patchRow"), "ends.some((n) => pan.contains(n))") {
		t.Error("difftok.js: patchRow does not leave the cells holding the selection's ends")
	}
}

// Cells held in an early slice are re-checked when the last slice is done:
// a selection collapsed meanwhile fired its selectionchange before the
// retry listener existed.
func TestPatchCellsRetriesHeldCellsAtTheEnd(t *testing.T) {
	t.Parallel()
	if !strings.Contains(jsFunc(t, "difftok.js", "patchRows"), `document.addEventListener("selectionchange", retry);
      retry();`) {
		t.Error("difftok.js: patchRows does not re-check its held rows once the last slice is done")
	}
}

// Leaving a diff for the list without overwriting it still ends it.
func TestDiffExitsToListCancelColours(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, fn string }{{"files.js", "drillOut"}, {"reviews.js", "goBack"}} {
		if !strings.Contains(jsFunc(t, c.file, c.fn), "abortDiffTok()") {
			t.Errorf("%s: %s leaves the diff without abortDiffTok()", c.file, c.fn)
		}
	}
}

// A text shown as LINE ROWS (blame, a file's content) takes its late runs
// row by row: row i is source line i+1.
func TestMergeLineToksJS(t *testing.T) {
	t.Parallel()
	var got struct {
		Changed []int            `json:"changed"`
		Lines   []map[string]any `json:"lines"`
	}
	pureJS(t, "difftok.js", "// --- merge toks (pure; guarded against Go) ---", "// --- end merge toks ---", `(() => {
  const lines = [{ text: "package p" }, { text: "" }, { text: "var x" }];
  const changed = mergeLineToks(lines, { tok: [[[0, 7, "kw"]], null, [[0, 3, "kw"]], [[0, 1, "kw"]]] });
  return { changed, lines };
})()`, &got)
	if !reflect.DeepEqual(got.Changed, []int{0, 2}) {
		t.Fatalf("changed = %v; want [0 2]", got.Changed)
	}
	if got.Lines[0]["tok"] == nil || got.Lines[1]["tok"] != nil || got.Lines[2]["tok"] == nil || len(got.Lines) != 3 {
		t.Fatalf("lines = %v", got.Lines)
	}
}

// A diff shown as ONE column (the viewer's): a deleted row takes the old
// side's runs by its old line, every other row the new side's by its new.
func TestMergeUnifiedToksJS(t *testing.T) {
	t.Parallel()
	var got struct {
		Changed []int            `json:"changed"`
		Rows    []map[string]any `json:"rows"`
	}
	pureJS(t, "difftok.js", "// --- merge toks (pure; guarded against Go) ---", "// --- end merge toks ---", `(() => {
  const rows = [{ i: 0, kind: "same", old: 1, nu: 1 }, { i: 1, kind: "del", old: 2, nu: 0 }, { i: 2, kind: "add", old: 0, nu: 2 }, { i: 3, kind: "same", old: 3, nu: 3 }];
  const changed = mergeUnifiedToks(rows, { old: [null, [[0, 1, "kw"]], null], new: [[[0, 2, "str"]], [[1, 2, "num"]], null] });
  return { changed, rows };
})()`, &got)
	if !reflect.DeepEqual(got.Changed, []int{0, 1, 2}) {
		t.Fatalf("changed = %v; want [0 1 2]", got.Changed)
	}
	if got.Rows[1]["tok"] == nil || got.Rows[2]["tok"] == nil || got.Rows[3]["tok"] != nil {
		t.Fatalf("rows = %v", got.Rows)
	}
}
