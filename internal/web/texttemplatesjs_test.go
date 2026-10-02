package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The decisions of the text-templates overlay, run under node: when the
// add/edit form may be left, what its notice line says, which keys answer the
// delete question, and which row a reload lands on.
const textTemplatesHarness = `
import { ttFormContent, ttLeaveForm, ttFormNotice, ttConfirmKey, ttSelectIndex, ttAnswerLanding } from "./tt.mjs";
const seed = ttFormContent("Title", "text");
const edited = ttFormContent("Title", "text more");
const rows = [{ id: "z", scope: "global" }, { id: "a", scope: "global" }, { id: "b", scope: "global" }, { id: "a", scope: "repo" }];
console.log(JSON.stringify({
  untouched: ttLeaveForm(seed, undefined, ttFormContent("Title", "text")),
  first: ttLeaveForm(seed, undefined, edited),
  again: ttLeaveForm(seed, edited, edited),
  editedAfterNotice: ttLeaveForm(seed, edited, ttFormContent("Title 2", "text more")),
  titleCounts: ttLeaveForm(seed, undefined, ttFormContent("Other", "text")),
  noticeBoth: ttFormNotice("not saved: boom", true),
  noticeErr: ttFormNotice("not saved: boom", false),
  noticeOnly: ttFormNotice("", true),
  noticeNone: ttFormNotice("", false),
  keys: ["y", "n", "Escape", "Y", "Enter", "Shift", "Tab", " "].map(ttConfirmKey),
  selRepo: ttSelectIndex(rows, "a", "repo", 2),
  selGlobal: ttSelectIndex(rows, "a", "global", 2),
  selGone: ttSelectIndex(rows, "zz", "repo", 2),
  selClamped: ttSelectIndex(rows.slice(0, 2), "", "", 5),
  selEmpty: ttSelectIndex([], "", "", 3),
  landings: [[true, false], [false, true], [false, false], [true, true]].map(([same, browsing]) => ttAnswerLanding(same, browsing)),
}));
`

func TestTextTemplatesOverlayDecisions(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	names := []string{"ttFormContent", "ttLeaveForm", "ttFormNotice", "ttConfirmKey", "ttSelectIndex", "ttAnswerLanding"}
	var mod strings.Builder
	for _, n := range names {
		mod.WriteString(jsFunc(t, "texttemplates.js", n) + "\n")
	}
	mod.WriteString("export { " + strings.Join(names, ", ") + " };\n")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tt.mjs"), []byte(mod.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(textTemplatesHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		Untouched, First, Again, EditedAfterNotice, TitleCounts string
		NoticeBoth, NoticeErr, NoticeOnly, NoticeNone           string
		Keys, Landings                                          []string
		SelRepo, SelGlobal, SelGone, SelClamped, SelEmpty       *int
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("not the harness JSON: %v\n%s", err, out)
	}
	// An untouched form goes at once; a changed one is kept once, and goes
	// the second time unless it was edited in between.
	if got.Untouched != "leave" || got.First != "warn" || got.Again != "leave" || got.EditedAfterNotice != "warn" || got.TitleCounts != "warn" {
		t.Errorf("leave: untouched %q first %q again %q edited-after %q title %q",
			got.Untouched, got.First, got.Again, got.EditedAfterNotice, got.TitleCounts)
	}
	// The notice names no key (esc, the cancel button and a click outside
	// all leave) and never replaces a pending error.
	const note = "unsaved text — leaving again discards it"
	if got.NoticeBoth != "not saved: boom · "+note || got.NoticeErr != "not saved: boom" || got.NoticeOnly != note || got.NoticeNone != "" {
		t.Errorf("notice: both %q err %q only %q none %q", got.NoticeBoth, got.NoticeErr, got.NoticeOnly, got.NoticeNone)
	}
	// Escape never reaches the question's own keys: the overlay's Escape
	// handling backs out of every step before them.
	if want := "delete,cancel,,,,,,"; strings.Join(got.Keys, ",") != want {
		t.Errorf("confirm keys = %q, want %q", strings.Join(got.Keys, ","), want)
	}
	// A late answer (a save, a delete, the list reload after either) acts in
	// the step that asked while it is still open; back at the list it may
	// redraw the list; with ANOTHER step open it touches nothing on screen.
	if want := "step,browse,elsewhere,step"; strings.Join(got.Landings, ",") != want {
		t.Errorf("save landings = %q, want %q", strings.Join(got.Landings, ","), want)
	}
	// The same id may live in both scopes: the scope picks the row.
	for name, c := range map[string]struct {
		got  *int
		want int
	}{"repo": {got.SelRepo, 3}, "global": {got.SelGlobal, 1}, "gone": {got.SelGone, 2}, "clamped": {got.SelClamped, 1}, "empty": {got.SelEmpty, 0}} {
		if c.got == nil || *c.got != c.want {
			t.Errorf("select %s = %v, want %d", name, c.got, c.want)
		}
	}
}

// A switch of the scope alone is not unsaved text (user ruling): the form's
// content is its title and text, and both its exits ask the same guard.
func TestTextTemplatesFormContentIgnoresScope(t *testing.T) {
	t.Parallel()
	if fn := jsFunc(t, "texttemplates.js", "ttFormContent"); strings.Contains(fn, "scope") {
		t.Errorf("ttFormContent reads the scope:\n%s", fn)
	}
	if fn := jsFunc(t, "texttemplates.js", "formContent"); strings.Contains(fn, "scope") || !strings.Contains(fn, "ttFormContent(") {
		t.Errorf("formContent must hand the title and the text to ttFormContent:\n%s", fn)
	}
}
