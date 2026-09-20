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
	if !strings.Contains(det, `postJSON("/api/pr/details?n="`) {
		t.Error("prdetails.js must POST the details load (R2: a GET never calls the forge)")
	}
	for _, bad := range []string{"refs/gg", "link_source", "linkSource"} {
		if strings.Contains(det, bad) || strings.Contains(prs, bad) {
			t.Errorf("a PR module mentions %q — the pair never travels back to the server", bad)
		}
	}
}
