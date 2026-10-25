package web

import (
	"strings"
	"testing"
)

// The chip's words are the TUI's commitScopeLabel: "solo: <entry>" for one
// (a commit entry shown short), "<n> branches" for several — "branches"
// even when a member is a tag or a sha, as there.
func TestScopeLabelJS(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("ab", 20)
	src := jsFunc(t, "commits.js", "soloLabel") + jsFunc(t, "commits.js", "scopeLabel") + `
console.log(JSON.stringify([
  scopeLabel([]),
  scopeLabel([{ kind: "branch", ref: "feat/x" }]),
  scopeLabel([{ kind: "commit", ref: "` + sha + `" }]),
  scopeLabel([{ kind: "commit", ref: "` + sha + `" }, { kind: "branch", ref: "main" }]),
  scopeLabel([{ kind: "branch", ref: "a" }, { kind: "branch", ref: "b" }, { kind: "branch", ref: "c" }]),
]));
`
	want := `["","solo: feat/x","solo: abababab","2 branches","3 branches"]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("scopeLabel = %s, want %s", got, want)
	}
}

// inScope answers whether a branch is a member of the commit view.
func TestInScopeJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "commits.js", "inScope") + `
const state = { scope: [{ kind: "branch", ref: "main" }, { kind: "commit", ref: "feat" }] };
console.log(JSON.stringify([inScope(state, "main"), inScope(state, "feat"), inScope(state, "x"), inScope({ scope: [] }, "main")]));
`
	if got := nodeRun(t, src); got != `[true,false,false,false]` {
		t.Errorf("inScope = %s", got)
	}
}

func TestScopeClientWiring(t *testing.T) {
	t.Parallel()
	commits := readStaticSrc(t, "commits.js")
	sidebar := readStaticSrc(t, "sidebar.js")
	reviews := readStaticSrc(t, "reviews.js")
	html := readStaticSrc(t, "index.html")
	for _, c := range []struct{ src, want, why string }{
		{commits, `setSoloChip(body.solo || "", body.solo_kind || "", body.scope || [])`, "the feed answer carries the whole list"},
		{commits, `el.textContent = label + " ✕"`, "the chip shows scopeLabel's words"},
		{commits, `postJSON("/api/solo", { op, kind, ref })`, "add / remove post an explicit op"},
		{commits, `label: "show all branches"`, "the commit menu offers Show all branches"},
		{sidebar, `label: "add to commit view"`, "the branch menu adds"},
		{sidebar, `label: "remove from commit view"`, "the branch menu removes"},
		{sidebar, `label: "show all branches"`, "the branch menu clears"},
		{sidebar, `class="scope-mark"`, "a member branch row carries ◉"},
		{reviews, `state.scope.map((e) => e.ref)`, "review visibility follows the whole list"},
		{html, `<span class="hkey">commit view</span>`, "help names the commit view"},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("%s: missing %q", c.why, c.want)
		}
	}
}
