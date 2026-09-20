package web

import (
	"regexp"
	"strings"
	"testing"
)

// R3: gg web has no global .hidden — an element born hidden with no #id.hidden
// rule is always visible. Every search element that hides is pinned here.
func TestPRSearchHiddenElementsHaveRules(t *testing.T) {
	t.Parallel()
	html, css := readStatic(t, "index.html"), readStatic(t, "style.css")
	for _, id := range []string{"pr-search-box", "pr-search-results"} {
		if !regexp.MustCompile(`id="` + id + `" class="hidden"`).MatchString(html) {
			t.Errorf("#%s must be born hidden", id)
		}
		if !strings.Contains(css, "#"+id+".hidden") {
			t.Errorf("style.css has no #%s.hidden rule", id)
		}
	}
	// The box folds with the section: the fold is a class on the list after it.
	if !strings.Contains(css, "#pr-search-box:has(+ #prs-list.collapsed)") {
		t.Error("the search box does not fold with the section")
	}
	if !regexp.MustCompile(`(?s)id="pr-search-box".*?</div>\s*<ul id="prs-list"`).MatchString(html) {
		t.Error("#pr-search-box must sit right before #prs-list (the fold rule is an adjacent-sibling one)")
	}
}

// R2 on the page: the search is a POST, the boot read a GET, and the results
// share the list's row painter.
func TestPRSearchPageWiring(t *testing.T) {
	t.Parallel()
	js := readStatic(t, "prs.js")
	for _, want := range []string{
		`postJSON("/api/pr/search"`, `getJSON("/api/pr/search")`, `runOnce("pr-search"`,
		"window.__ggFocusPRSearch = focusPRSearch", "sr.prs.map((pr) => prRowHTML(pr, now))",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("prs.js lacks %s", want)
		}
	}
	if strings.Count(js, "function prRowHTML") != 1 || !strings.Contains(js, "rows.map((pr) => prRowHTML(pr, now))") {
		t.Error("the list and the results must share prRowHTML")
	}
	if !strings.Contains(readStatic(t, "keys.js"), `e.key === "A"`) {
		t.Error("keys.js does not bind A")
	}
}
