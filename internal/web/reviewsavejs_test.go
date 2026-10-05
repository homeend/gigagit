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
const openReport = (title, noteId, content) => calls.push("open:" + noteId + ":" + content);
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
	want := "open::R|line:review failed: review not saved: notes are disabled|" +
		"line:AI resolve failed: boom|" +
		"open::P|line:review failed: review not saved"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("calls =\n%s\nwant\n%s", got, want)
	}
}
