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
		`"copy absolute file path"`,
		`"copy repo absolute path"`,
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
	// With nothing selected the row under the pointer is offered instead —
	// the CELL, since a side-by-side row holds both versions of the line.
	if !strings.Contains(src, `{ label: "copy line", act: () => copyText(line, "line") }`) {
		t.Error("the diff's right-click menu no longer offers the line under the pointer")
	}
	if !strings.Contains(src, `e.target.closest("td.side")`) {
		t.Error("`copy line` no longer reads the cell under the pointer — it would copy both sides of a split row")
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

// Opening a file parks the view on its first changed line: the context above
// the first hunk can run for screens. It must happen at the three OPEN sites
// and nowhere else — renderDiff also runs on a window resize and on every
// notes refresh, where a jump would yank the view out from under a reader.
func TestJumpToFirstChangeOnOpenOnly(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, "function jumpToFirstChange()") {
		t.Fatal("files.js: jumpToFirstChange is gone — an opened diff starts at the top of the file again")
	}
	if n := strings.Count(src, "jumpToFirstChange();"); n != 3 {
		t.Errorf("jumpToFirstChange is called %d times, want 3 (commit file, working-tree file, entry/compare diff)", n)
	}
	// The body of renderDiff must not call it.
	i := strings.Index(src, "function renderDiff(d) {")
	j := strings.Index(src[i:], "\n}\n")
	if i < 0 || j < 0 {
		t.Fatal("files.js: renderDiff is no longer recognizable")
	}
	if strings.Contains(src[i:i+j], "jumpToFirstChange") {
		t.Error("renderDiff jumps to the first change — a window resize or a notes refresh would move the reader's view")
	}
}

// The top bar's refresh button is styled with pull and push, not left on the
// browser's default button look.
func TestRefreshButtonStyled(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	for _, want := range []string{
		"#top #pull-btn, #top #push-btn, #top #refresh-btn {",
		"#top #refresh-btn:hover:not(:disabled)",
		"#top #refresh-btn:disabled",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css is missing %q — the refresh button does not match pull/push", want)
		}
	}
}

// The file-list header's right-click menu is ONE list for the whole header —
// clicking the sha versus the date must not change what is offered — and a
// selection anywhere in it collapses the menu to a plain "copy". Both halves
// are easy to lose to a well-meant "make the menu contextual" edit, and the
// symptom is only ever a missing row.
func TestCommitHeaderMenuRows(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		`"copy short commit id"`,
		`"copy commit id"`,
		`"copy commit title"`,
		`"copy date"`,
		`"copy author"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("files.js no longer offers %s in the commit header's menu", want)
		}
	}
	// The date and the author are copied from the values the stamp was built
	// from, never parsed back out of the rendered " · " line.
	if !strings.Contains(src, "function commitMetaParts(body)") {
		t.Error("commitMetaParts is gone — the date/author rows would have to re-parse the rendered line")
	}
	if !strings.Contains(src, "meta.dataset.date") || !strings.Contains(src, "meta.dataset.author") {
		t.Error("the header menu no longer reads the date/author off the element")
	}
	// The handler must not gate on which part of the header was clicked.
	if strings.Contains(src, `!e.target.closest("#files-title")`) {
		t.Error("the header menu is gated on hitting the title again — right-clicking the date would offer nothing")
	}
	if !strings.Contains(src, `showCtxMenu([{ label: "copy", act: () => copyText(text, "selection") }], e.clientX, e.clientY)`) {
		t.Error("a selection in the header no longer collapses the menu to a plain copy")
	}
}

// The header reads as chrome, so it takes the default arrow rather than the
// I-beam a bare run of text would get.
func TestFilesHeaderCursor(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "#files-header { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; cursor: default; }") {
		t.Error("style.css: #files-header lost `cursor: default` — it shows a text I-beam over chrome")
	}
}
