package web

import (
	"strings"
	"testing"
)

// The landing mark (the TUI's Branches cursor after a walk): a Branches
// sub-row open lands on its session, another open clears a mark naming a
// different session, and a worktree's landing row is its checked-out branch.
func TestWalkLandingJS(t *testing.T) {
	t.Parallel()
	var mod strings.Builder
	for _, fn := range []string{"pathKey", "landingAfterOpen", "worktreeBranch"} {
		mod.WriteString(jsFunc(t, "walk.js", fn) + "\n")
	}
	src := mod.String() + `
const wts = [{path: "/r", branch: "main"}, {path: "/r/wt-a", branch: "wta"}, {path: "/r/wt-b", branch: "", detached: true}];
console.log(JSON.stringify([
  landingAfterOpen(null, "s1", true),           // a Branches sub-row: the mark lands on it
  landingAfterOpen({sid: "s2"}, "s1", true),
  landingAfterOpen({sid: "s2"}, "s1", false),   // another open: a mark naming another session goes
  landingAfterOpen({sid: "s1"}, "s1", false),   // …one naming this session stays
  landingAfterOpen({branch: "wta"}, "s1", false), // a branch mark stays (the TUI cursor does)
  landingAfterOpen(null, "s1", false),
  worktreeBranch(wts, "/r/wt-a/"),              // a spelling, not another worktree
  worktreeBranch(wts, "/r/wt-b"),               // detached: no branch row
  worktreeBranch(wts, "/elsewhere"),
  worktreeBranch(null, "/r"),
]));
`
	want := `[{"sid":"s1"},{"sid":"s1"},null,{"sid":"s1"},{"branch":"wta"},null,"wta","","",""]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("landing =\n%s\nwant\n%s", got, want)
	}
}

// landedClass is the row's class from the mark: a session sub-row by id, a
// branch row by name, nothing else.
func TestLandedClassJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "sidebar.js", "landedClass") + `
console.log(JSON.stringify([
  landedClass({sid: "s1"}, "sid", "s1"),
  landedClass({sid: "s1"}, "sid", "s2"),
  landedClass({sid: "s1"}, "branch", "s1"),
  landedClass({branch: "wta"}, "branch", "wta"),
  landedClass({branch: "wta"}, "sid", "wta"),
  landedClass(null, "branch", "wta"),
  landedClass({branch: ""}, "branch", ""),
]));
`
	want := `[" landed","",""," landed","","",""]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("landedClass =\n%s\nwant\n%s", got, want)
	}
}

// The mark is state, painted by renderBranches (so a repaint keeps it), with
// its own rule; a landing reveals the row.
func TestWalkLandingPainted(t *testing.T) {
	t.Parallel()
	core := readStaticSrc(t, "core.js")
	side := readStaticSrc(t, "sidebar.js")
	css := readStaticSrc(t, "style.css")
	for _, pin := range []struct{ src, want, why string }{
		{core, "landing: null", "state.landing starts empty"},
		{side, `landedClass(state.landing, "branch", b.name)`, "a branch row is painted from the mark"},
		{side, `landedClass(state.landing, "sid", r.id)`, "a session sub-row is painted from the mark"},
		{side, "function land(", "land sets the mark"},
		{side, `if (isCollapsed("branches")) toggleSection("branches")`, "a landing unfolds the branches section"},
		{side, `scrollIntoView({ block: "nearest" })`, "a landing scrolls its row into view"},
		{side, "clearLandingUnless(", "a click on another Branches row clears the mark"},
		{css, "#branches-list li.landed", "the mark has its own rule"},
	} {
		if !strings.Contains(pin.src, pin.want) {
			t.Errorf("%s: missing %q", pin.why, pin.want)
		}
	}
	if !strings.Contains(side, "land,") && !strings.Contains(side, " land ") {
		t.Errorf("sidebar.js must export land")
	}
}
