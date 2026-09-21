package web

import (
	"strings"
	"testing"
)

// The stacked view fetches each file through the SAME URL builder the
// single-file view uses, so the two can never disagree on what a row's diff
// is. A regression here is a stack that quietly shows a different diff.
func TestFileDiffURLIsShared(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	if !strings.Contains(files, "function fileDiffURL(f)") {
		t.Fatal("files.js: fileDiffURL is gone")
	}
	if n := strings.Count(files, "getJSON(fileDiffURL("); n < 2 {
		t.Errorf("single-file opens use fileDiffURL %d times, want >= 2 (commit/compare and working tree)", n)
	}
	if strings.Contains(files, `getJSON("/api/diff?" + q)`) {
		t.Error("files.js builds a /api/diff URL inline again — route it through fileDiffURL")
	}
}

// The stack is only reachable if the doors route into it and every exit
// tears it down; each of these lines is one of those doors or exits.
func TestStackViewWired(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	for _, want := range []string{
		"if (stackOn()) return openStack(i);",      // openFile routes into the stack
		`if (mode !== "diff") teardownStack();`,    // leaving the diff stage drops it
		"if (state.stack) return rerenderStack();", // f / w / resize re-render the stack
		"reconcileStack();",                        // a status re-read keeps its sections
	} {
		if !strings.Contains(files, want) {
			t.Errorf("files.js is missing %q", want)
		}
	}
	view := readStatic(t, "stackview.js")
	for _, want := range []string{
		"new IntersectionObserver(",
		"STACK_MAX_IN_FLIGHT",
		`getJSON("/api/numstat?"`,
		"getJSON(fileDiffURL(",
		"state.ui && state.ui.stacked_diff",
		"saveUI({ stacked_diff:",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("stackview.js is missing %q", want)
		}
	}
	css := readStatic(t, "style.css")
	for _, want := range []string{".stk-head", "position: sticky", "var(--diff-head-h", "#stack-fold-all.hidden"} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css is missing %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "app.js"), `from "./stackview.js";`) {
		t.Error("app.js does not load stackview.js")
	}
	// A single-file diff still loading when S builds a stack must not paint
	// over it (found by the working-tree browser probe: click a file, press S
	// before its diff lands).
	if !strings.Contains(files, "// A single diff never paints over a stack") {
		t.Error("renderDiff lost its stack guard")
	}
	if n := strings.Count(files, "if (gen !== state.detailGen) return;"); n < 3 {
		t.Errorf("only %d opens drop a superseded answer, want the commit, working-tree and entry opens", n)
	}
}

func TestStackKeysWired(t *testing.T) {
	t.Parallel()
	keys := readStatic(t, "keys.js")
	for _, want := range []string{
		`e.key === "S"`, "toggleStacked()",
		`e.key === "-"`, "collapseCurrent()",
		`e.key === "_"`, "toggleAllCollapsed()",
		`case "stacked": toggleStacked(); break;`,
		"if (state.stack && state.layout === \"diff\") return openFile(state.fileCursor);",
	} {
		if !strings.Contains(keys, want) {
			t.Errorf("keys.js is missing %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "index.html"), `data-act="stacked"`) {
		t.Error("the footer has no stacked chip (advertise in help AND footer)")
	}
	if !strings.Contains(readStatic(t, "palette.js"), "toggleStacked()") {
		t.Error("the ☰ menu has no stacked-diff row")
	}
}
