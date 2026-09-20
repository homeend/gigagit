package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readStatic(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("static", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A cross-module call with no import is a ReferenceError the refresh lane's
// catch swallows (plan 4 shipped one). Every plan-5 call site is pinned.
func TestPRThreadCallsAreImported(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, ident, module string }{
		{"live.js", "refreshPRComments", "prs.js"},
		{"prs.js", "fetchNotes", "files.js"},
		{"prs.js", "loadPRCounts", "previews.js"},
		{"prs.js", "openPRDetails", "prdetails.js"},
		{"files.js", "noteTitle", "notebox.js"},
		{"files.js", "seedCollapsed", "notebox.js"},
		{"keys.js", "collapseNearestNote", "files.js"},
		{"keys.js", "toggleNoteCollapsed", "files.js"},
		{"app.js", "./prdetails.js", ""},
	} {
		js := readStatic(t, c.file)
		if c.module == "" {
			if !strings.Contains(js, c.ident) {
				t.Errorf("%s does not load %s", c.file, c.ident)
			}
			continue
		}
		if !strings.Contains(js, c.ident+"(") {
			t.Errorf("%s no longer calls %s", c.file, c.ident)
		}
		re := regexp.MustCompile(`import \{[^}]*\b` + c.ident + `\b[^}]*\} from "\./` + regexp.QuoteMeta(c.module) + `"`)
		if !re.MatchString(js) {
			t.Errorf("%s calls %s without importing it from ./%s", c.file, c.ident, c.module)
		}
	}
}

// The web hides by ID: an element born class="hidden" with no #id.hidden rule
// is always visible.
func TestPRDetailsOverlayIsBornHidden(t *testing.T) {
	t.Parallel()
	html, css := readStatic(t, "index.html"), readStatic(t, "style.css")
	if !strings.Contains(html, `<div id="prdetails" class="hidden">`) {
		t.Error(`index.html: #prdetails must start class="hidden"`)
	}
	if !regexp.MustCompile(`#prdetails\.hidden\s*\{\s*display:\s*none`).MatchString(css) {
		t.Error("style.css: no #prdetails.hidden rule — the overlay would cover the page from the start")
	}
}

func TestNoteCollapseAndForgeBoxesAreStyled(t *testing.T) {
	t.Parallel()
	css := readStatic(t, "style.css")
	for _, want := range []string{"tr.note.collapsed .notesum", "tr.note.collapsed .notereply", ".notebox.forge"} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css lacks %q", want)
		}
	}
}

// R1 + R2 on the page: a PR's notes, comments and details are asked for by
// number, and the two forge-spending calls are POSTs.
func TestPRThreadEndpointsOnThePage(t *testing.T) {
	t.Parallel()
	files, prs, det := readStatic(t, "files.js"), readStatic(t, "prs.js"), readStatic(t, "prdetails.js")
	if !strings.Contains(files, `"/api/pr/notes?"`) {
		t.Error("files.js does not read a PR's notes from /api/pr/notes")
	}
	if !strings.Contains(prs, `postJSON("/api/pr/comments/refresh?n="`) {
		t.Error("prs.js must POST the comment refresh")
	}
	// Stale-first: the overlay is pushed BEFORE any request, and asks for the
	// cached copy (a GET) before the fresh one.
	push, get, post := strings.Index(det, `pushLayer("prdetails"`), strings.Index(det, `getJSON("/api/pr/details?n="`), strings.Index(det, `postJSON("/api/pr/details?n="`)
	if push < 0 || get < push || post < get {
		t.Errorf("prdetails.js: want pushLayer < cached GET < fresh POST, got %d %d %d", push, get, post)
	}
	if !regexp.MustCompile(`#prdetails-status\.hidden\s*\{\s*display:\s*none`).MatchString(readStatic(t, "style.css")) {
		t.Error("style.css: no #prdetails-status.hidden rule")
	}
	if !strings.Contains(det, `postJSON("/api/pr/details?n="`) {
		t.Error("prdetails.js must POST the details load (R2: a GET never calls the forge)")
	}
	for _, bad := range []string{"refs/gg", "link_source", "linkSource"} {
		if strings.Contains(det, bad) || strings.Contains(prs, bad) {
			t.Errorf("a PR module mentions %q — the pair never travels back to the server", bad)
		}
	}
}

// The files title is one elided line; a PR's never fits. It must offer the
// whole text on hover, and the bar's file count must never be the part that
// elides ("all…" told nobody anything).
func TestPRHeaderIsReadable(t *testing.T) {
	t.Parallel()
	files, css := readStatic(t, "files.js"), readStatic(t, "style.css")
	if !regexp.MustCompile(`\$\("files-title"\)\.addEventListener\("mouseenter"`).MatchString(files) ||
		!strings.Contains(files, "el.scrollWidth > el.clientWidth ? el.textContent") {
		t.Error("files.js: a cut #files-title must show its full text as a tooltip")
	}
	if !regexp.MustCompile(`#compare-bar button\.cmpall \{[^}]*flex: none`).MatchString(css) || !strings.Contains(files, `class="on cmpall"`) {
		t.Error("the preview bar's file count must not shrink")
	}
}

// Forge text reaches the page's innerHTML through exactly two doors: the
// painter (a parsed tree) or esc() (the fallback). The token colours are
// selector-scoped, so the overlay has to be on their list or a fenced block
// there paints grey.
func TestPRMarkdownIsWiredAndStyled(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	details, files, css := read("prdetails.js"), read("files.js"), read("style.css")
	if !strings.Contains(details, `import { mdHTML } from "./markdown.js";`) {
		t.Error("prdetails.js must import the painter")
	}
	if !strings.Contains(files, `import { mdHTML, mdInlineHTML } from "./markdown.js";`) {
		t.Error("files.js must import the painter")
	}
	if n := strings.Count(details, "pr.body"); n != 2 || !strings.Contains(details, "textHTML(d.body_md, pr.body)") {
		t.Errorf("the description goes through textHTML only (pr.body seen %d times)", n)
	}
	if n := strings.Count(details, "c.body"); n != 1 || !strings.Contains(details, "textHTML(c.md, c.body)") {
		t.Errorf("a comment body goes through textHTML only (c.body seen %d times)", n)
	}
	if !strings.Contains(details, "mdHTML(md, esc)") || !strings.Contains(details, "esc(raw || \"\")") {
		t.Error("textHTML paints the tree or escapes the raw text")
	}
	for _, rule := range []string{"#prdetails .tk-kw", "#prdetails .tk-str", "#prdetails .tk-cmt", ".md pre.md-code", ".md table.md-table", ".md { white-space: normal;"} {
		if !strings.Contains(css, rule) {
			t.Errorf("style.css misses %q", rule)
		}
	}
	if !strings.Contains(files, `if (e.target.closest("a[href]")) return;`) {
		t.Error("a link inside a note keeps the browser's own context menu")
	}
}
