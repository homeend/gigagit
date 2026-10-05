package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A review the store could not keep still shows: the done event fails with
// the save error and carries the report, and the page opens the report (no
// note id) beside the error line, whether the run was watched or parked.
const reviewSaveFailHarness = `
const calls = [];
let rev = null;
const state = { task: null };
const reviewTitle = () => "Review";
const closeReviewLane = () => {};
const refreshAfterOp = () => {};
const hideOpLine = () => {};
const renderTaskChip = () => {};
const unparkReview = () => {};
const reviewDoc = () => null;
const openReport = (title, noteId, content, doc, unsaved) => calls.push("open:" + noteId + ":" + content + ":" + (unsaved || ""));
const opLine = (text) => calls.push("line:" + text);
%s
%s
const ev = { ok: false, error: "review not saved: notes are disabled", report: "R" };
reviewDone(ev, "review");
reviewDone({ ok: false, error: "boom", report: "C" }, "conflict_complete");
state.task = { kind: "review", status: "failed", title: "Review", report: "P", error: "review not saved" };
collectTask();
console.log(calls.join("|"));
`

func TestWebOpensAReviewTheStoreCouldNotKeep(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src := strings.Replace(reviewSaveFailHarness, "%s", jsFunc(t, "review.js", "reviewDone"), 1)
	src = strings.Replace(src, "%s", jsFunc(t, "review.js", "collectTask"), 1)
	run := filepath.Join(t.TempDir(), "run.mjs")
	if err := os.WriteFile(run, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, run).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := "open::R:review not saved: notes are disabled|line:review failed: review not saved: notes are disabled|" +
		"line:AI resolve failed: boom|" +
		"open::P:review not saved|line:review failed: review not saved"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("calls =\n%s\nwant\n%s", got, want)
	}
}

// The report viewer's path line says where the review is kept — or, for a
// review the store could not keep, that it was not saved: the error line
// under the viewer is covered while it is open.
func TestWebReportPathSaysAReviewWasNotSaved(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src := jsFunc(t, "review.js", "reportWhere") + `
console.log(JSON.stringify([reportWhere("ab12cd34", ""), reportWhere("", "review not saved: x"), reportWhere("", "")]));
`
	run := filepath.Join(t.TempDir(), "run.mjs")
	if err := os.WriteFile(run, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, run).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != `["note ab12cd34","not saved — review not saved: x",""]` {
		t.Errorf("where = %s", got)
	}
}

// ✎ marks a working-list row whose file a current review matches, except a
// Staged row: the review's notes are not drawn on the staged diff (user
// ruling), so ✎ there would point at notes the diff never shows.
func TestWebWorkingReviewMarkSkipsStagedRows(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src := jsFunc(t, "reviews.js", "workingReviewMarkHTML") + `
const reviewed = new Set(["a.txt"]);
console.log(JSON.stringify(["changes", "untracked", "staged"].map((section) => workingReviewMarkHTML({ path: "a.txt", section }, reviewed) !== "")
  .concat([workingReviewMarkHTML({ path: "b.txt", section: "changes" }, reviewed) !== ""])));
`
	run := filepath.Join(t.TempDir(), "run.mjs")
	if err := os.WriteFile(run, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, run).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != `[true,true,false,false]` {
		t.Errorf("marked (changes, untracked, staged, unreviewed) = %s, want [true,true,false,false]", got)
	}
	files, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files), "workingReviewMarkHTML(f, reviewed)") {
		t.Error("files.js renderFiles does not draw its ✎ through workingReviewMarkHTML")
	}
}
