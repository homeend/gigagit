package web

import (
	"reflect"
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
