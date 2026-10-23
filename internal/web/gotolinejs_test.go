package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gotoPureStart = "// --- gotoline model (pure; guarded against Go) ---"
const gotoPureEnd = "// --- end gotoline model ---"

// parseLineNo is the prompt's one parser (digits only, ≤ 7 of them, 0 =
// not a line) and newerSrc the spec's one rule for "open the file at that
// line" from a diff: the working tree or a commit, else nothing.
func TestGotoLineModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "gotoline.js", gotoPureStart, gotoPureEnd, `
const r = [];
for (const s of ["42", " 007 ", "", "0", "4a", "-3", "12345678", "1234567"]) r.push(parseLineNo(s));
for (const c of [null, {state:"commit",rev:"abc"}, {state:"commit",rev:""}, {state:"unstaged"}, {state:"untracked"}, {state:"staged"}, {cmpSides:{}, state:"commit", rev:"abc"}]) r.push(JSON.stringify(newerSrc(c)));
console.log(r.join("|"));
`)
	want := `42|7|0|0|0|0|0|1234567|null|{"src":"commit","rev":"abc"}|null|{"src":"worktree"}|{"src":"worktree"}|null|null`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// : is wired into every web surface that shows a file, and advertised.
func TestGotoLineIsWiredEverywhere(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"gotoline.js", `export function openGotoLine(`, "the one prompt every surface opens"},
		{"gotoline.js", `title: "Go to line"`, "the dialog's title"},
		{"viewer.js", `case ":": if (!gotoViewerLine()) return false; break;`, "the viewer's file keys open it"},
		{"viewer.js", `function viewerGoToLine(`, "the viewer's landing"},
		{"viewer.js", `case ":": gotoStackLine(st); break;`, "the review stack opens it"},
		{"viewer.js", `data-vact="goto">: line</button>`, "the viewer's footer advertises it"},
		{"files.js", `function gotoDiffKey(`, "the diff pane's key"},
		{"files.js", `newerSrc(ctx)`, "a hidden line opens the file only on the spec's rule"},
		{"files.js", `landStackLine(`, "a stacked diff lands in its file"},
		{"keys.js", `if (gotoDiffKey(e)) return;`, "the diff layout routes :"},
		{"filehist.js", `e.key === ":"`, "blame and history open it"},
		{"filehist.js", `openViewer({ src: "commit"`, "history opens the commit's file"},
		{"filehist.js", `classList.add("goto")`, "blame marks the landed row"},
		{"style.css", `#blame-body .bline.goto`, "the landed blame row is visible"},
		{"index.html", `: line`, "the diff/blame/history hints advertise it"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	for _, f := range []string{"viewer.js", "files.js", "filehist.js"} {
		if !strings.Contains(read(f), `": · go to line`) && !strings.Contains(read(f), `: · go to line`) {
			t.Errorf("%s: the ? help must advertise : (go to line)", f)
		}
	}
}
