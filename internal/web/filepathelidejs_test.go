package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The file list cuts long paths the way the TUI does — whole segments out of
// the MIDDLE (elidePath), so the file name survives — rather than letting the
// CSS `text-overflow: ellipsis` drop the tail. The regression this guards is
// a new render path (or a merge) writing the raw path back into a row: the
// list still looks fine on a wide pane and silently goes back to hiding the
// file name on a narrow one.
func TestFileRowsElidePaths(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"function filePathHTML(", // the one place a row's path is rendered
		"elidePath(path, cols)",  // …and it elides
		"new ResizeObserver(",    // a widened pane re-elides
	} {
		if !strings.Contains(src, want) {
			t.Errorf("files.js no longer contains %q — the file list's middle-elision is gone", want)
		}
	}
	// Two render branches (commit/compare and the working tree) put a path in
	// a row; both must go through the helper.
	if n := strings.Count(src, "filePathHTML(f.path, cols)"); n != 2 {
		t.Errorf("filePathHTML is used on %d file rows, want 2 (the commit list and the working tree)", n)
	}
	if strings.Contains(src, `"</span>${esc(f.path)}`) || strings.Contains(src, `>${esc(f.path)}<`) {
		t.Error("a file row writes esc(f.path) straight into the list again — it will be cut at the tail")
	}
}

// The header's path and the header's commit id are copy targets. Their menus
// are the only way to get either out of the browser, so a lost listener is a
// lost feature with no other symptom.
func TestHeaderCopyMenusWired(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		`$("diff-header").addEventListener("click"`,
		`$("diff-header").addEventListener("contextmenu"`,
		`$("files-header").addEventListener("contextmenu"`,
		`id="diff-path"`,
		`"copy short commit id"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("files.js no longer contains %q — a header copy affordance is gone", want)
		}
	}
}

// A selection inside the diff gets its own "copy" row: the browser's native
// menu is suppressed as soon as gg has anything of its own to offer, so
// without this row a right-click over selected text offers no way to copy it.
func TestDiffSelectionCopyRow(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, `{ label: "copy", act: () => copyText(text, "selection") }`) {
		t.Error("the diff's right-click menu no longer offers the selected text")
	}
	// The text must be read at menu-open time: by the time the row's act()
	// runs the selection is gone.
	if !strings.Contains(src, "const text = sel && !sel.isCollapsed") {
		t.Error("the selection is no longer captured when the menu opens")
	}
	// The line-number gutter stays unselectable, or the copied text comes
	// back interleaved with line numbers.
	css, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), "td.no") || !strings.Contains(string(css), "user-select: none") {
		t.Error("style.css: td.no lost `user-select: none` — copying a selection would include line numbers")
	}
}
