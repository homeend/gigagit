package web

import (
	"os"
	"path/filepath"
	"regexp"
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
