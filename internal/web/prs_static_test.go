package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// No usable forge means no pull-request UI at all, so the section is BORN
// hidden and only prs.js un-hides it. This stylesheet hides by id — a bare
// class="hidden" with no "#id.hidden" rule of its own is always visible (a
// bug this app has shipped before) — so the rule is pinned here, together
// with the markup that relies on it.
func TestPRSectionIsBornHidden(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	html, css, js := read("index.html"), read("style.css"), read("prs.js")

	for _, id := range []string{"prs-header", "prs-list"} {
		re := regexp.MustCompile(`<[a-z]+ id="` + id + `" class="[^"]*\bhidden\b[^"]*"`)
		if !re.MatchString(html) {
			t.Errorf("index.html: #%s must carry class hidden in the markup", id)
		}
		if n := strings.Count(html, `id="`+id+`"`); n != 1 {
			t.Errorf("index.html: %d elements with id %q, want exactly 1", n, id)
		}
		rule := regexp.MustCompile(`#` + id + `\.hidden[^{]*\{[^}]*display:\s*none`)
		if !rule.MatchString(css) {
			t.Errorf("style.css: no `#%s.hidden { display: none }` rule — the section would always show", id)
		}
		if !strings.Contains(js, `$("`+id+`").classList.toggle("hidden", !state.prsAvailable)`) {
			t.Errorf("prs.js: #%s must be shown only on an available answer", id)
		}
	}
	if !(strings.Index(html, `id="previews-list"`) < strings.Index(html, `id="prs-header"`) &&
		strings.Index(html, `id="prs-list"`) < strings.Index(html, `id="tags-header"`)) {
		t.Error("index.html: the pull-requests section sits between previews and tags")
	}
}

// live.js once USED fetchPRs without importing it: the ReferenceError was
// swallowed by the refresh lane's catch, so the interval re-list never reached
// the page — and only a browser run showed it. Every module that calls it
// must import it.
func TestFetchPRsIsImportedWhereItIsCalled(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"live.js", "app.js"} {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		js := string(b)
		if !strings.Contains(js, "fetchPRs()") {
			t.Errorf("%s no longer calls fetchPRs — the pull-request list would not follow it", name)
		}
		if !regexp.MustCompile(`import \{[^}]*\bfetchPRs\b[^}]*\} from "\./prs\.js"`).MatchString(js) {
			t.Errorf("%s calls fetchPRs without importing it from ./prs.js", name)
		}
	}
}

// The page names a pull request by NUMBER. Nothing in prs.js may put a ref or
// a branch name on a PR endpoint's query string.
func TestPRPageSendsOnlyTheNumber(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "prs.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, bad := range []string{"refs/gg", "source=", "target=", "head_sha"} {
		if strings.Contains(js, bad) {
			t.Errorf("prs.js mentions %q — a PR is addressed by its number only", bad)
		}
	}
	if !strings.Contains(js, `"/api/pr/open?n=" + n`) {
		t.Error("prs.js: the open read must be /api/pr/open?n=<number>")
	}
}

// The loading mask dims the PANES; it is not a dialog. Anything the user opens
// while a pull request loads — the main menu, a context menu, the palette, a
// decision modal — must paint above it, so its z-index stays below every
// overlay's (a mask at the menu's own 40 once painted over the open menu).
func TestPRMaskSitsBelowEveryOverlay(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	zOf := func(id string) int {
		m := regexp.MustCompile(`(?m)^[^{\n]*#` + id + `\b[^{\n]*\{[^}]*z-index:\s*(\d+)`).FindStringSubmatch(css)
		if m == nil {
			t.Fatalf("style.css: no z-index rule for #%s", id)
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}
	mask := zOf("pr-mask")
	for _, id := range []string{"ctx-menu", "modal", "preflight", "palette", "prompt", "help", "settings", "prdetails", "history", "blame"} {
		if z := zOf(id); mask >= z {
			t.Errorf("#pr-mask z-index %d must be below #%s's %d", mask, id, z)
		}
	}
}
