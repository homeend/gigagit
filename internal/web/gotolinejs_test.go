package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gotoPureStart = "// --- gotoline model (pure; guarded against Go) ---"
const gotoPureEnd = "// --- end gotoline model ---"

// parseLineNo is the prompt's one parser (digits only, the first 7 of them
// count — the TUI's prompt takes no more — 0 = not a line), gotoAnswer what
// enter does with the text (a line, a quiet cancel on empty or 0, or why
// not), and newerSrc the spec's one rule for "open the file at that line"
// from a diff: the working tree or a commit, else nothing — a compare's
// right side or a working review's marker included, as the TUI's newerSide.
func TestGotoLineModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "gotoline.js", gotoPureStart, gotoPureEnd, `
const r = [];
for (const s of ["42", " 007 ", "", "0", "4a", "-3", "12345678", "1234567"]) r.push(parseLineNo(s));
for (const c of [null, {state:"commit",rev:"abc"}, {state:"commit",rev:""}, {state:"unstaged"}, {state:"untracked"}, {state:"staged"}, {cmpSides:{}, state:"commit", rev:"abc"},
  {state:"commit",rev:"",newWorktree:true}, {cmpSides:{left:"commit:a",right:"worktree"}}, {cmpSides:{left:"worktree",right:"commit:abc"}}, {cmpSides:{left:"commit:a",right:"staged"}}, {cmpSides:{left:"commit:a",right:"shelf:x"}}]) r.push(JSON.stringify(newerSrc(c)));
for (const s of ["42", "", "  ", "0", "000", "4a", "12345678"]) r.push(JSON.stringify(gotoAnswer(s)));
console.log(r.join("|"));
`)
	want := `42|7|0|0|0|0|1234567|1234567|null|{"src":"commit","rev":"abc"}|null|{"src":"worktree"}|{"src":"worktree"}|null|null|` +
		`{"src":"worktree"}|{"src":"worktree"}|{"src":"commit","rev":"abc"}|null|null|` +
		`{"n":42,"why":""}|{"n":0,"why":""}|{"n":0,"why":""}|{"n":0,"why":""}|{"n":0,"why":""}|{"n":0,"why":"type a line number (1 or more)"}|{"n":1234567,"why":""}`
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
		{"files.js", `if (!state.stack && !$("diff-body").querySelector("tr[data-no]")) return false;`, "no prompt over a diff that is loading, failed, binary or an image pair: no numbered rows yet"},
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
