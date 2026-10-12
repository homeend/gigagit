package web

import (
	"strings"
	"testing"
)

const orPureStart = "// --- overview rows (pure; guarded against Go) ---"
const orPureEnd = "// --- end overview rows ---"

func TestOverviewRowsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "overviews.js", orPureStart, orPureEnd, `
const ovs = [
  { id: "o2", commit: "abc1234def", branch: "feat", agent: "Claude", title: "Parsing", created: "2026-10-12T09:40:00Z" },
  { id: "o1", commit: "abc1234def", branch: "feat", agent: "Claude", title: "Locks", created: "2026-10-11T08:00:00Z" },
  { id: "o3", commit: "fff0000aaa", branch: "feat", agent: "Claude", title: "Old tip", created: "2026-10-10T08:00:00Z" },
];
console.log([
  commitOverviews(ovs, "abc1234def").map((o) => o.id).join("+"),
  commitOverviews(ovs, "0000").length,
  branchOverviews(ovs, { name: "feat", hash: "abc1234" }).map((o) => o.id).join("+"),
  branchOverviews(ovs, { name: "main", hash: "abc1234" }).length,
  overviewRowText(ovs[1], new Date("2026-10-12T12:00:00Z")).startsWith("└ Overview: Locks "),
].join("|"));
`)
	if want := "o2+o1|0|o2+o1|0|true"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestOverviewSurfacesAreWired(t *testing.T) {
	t.Parallel()
	c := readStatic(t, "commits.js")
	for _, want := range []string{`class="ovmark"`, `commitOverviews(`, `"open overview: " + `} {
		if !strings.Contains(c, want) {
			t.Errorf("commits.js lacks %q", want)
		}
	}
	s := readStatic(t, "sidebar.js")
	for _, want := range []string{`class="bov" data-overview=`, `li.dataset.overview`, `overviewMenu(`} {
		if !strings.Contains(s, want) {
			t.Errorf("sidebar.js lacks %q", want)
		}
	}
	o := readStatic(t, "overviews.js")
	for _, want := range []string{`"Open overview"`, `"Delete overview"`, `"/api/overview/remove"`} {
		if !strings.Contains(o, want) {
			t.Errorf("overviews.js lacks %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "style.css"), `.crow .ovmark`) {
		t.Error("style.css lacks the ≡ mark's rule")
	}
	if !strings.Contains(readStatic(t, "index.html"), `open overview:`) {
		t.Error("index.html help lacks the overview rows")
	}
}

// R10: a PR view's Overviews block — its rows come from /api/pr/notes only,
// are cleared wherever the Reviews block is, and route click / right-click /
// enter to the overview.
func TestPRViewOverviewsAreWired(t *testing.T) {
	t.Parallel()
	pv := readStatic(t, "previews.js")
	if !strings.Contains(pv, `state.previewOverviews = d.overviews || []`) {
		t.Error("previews.js does not keep the PR's overviews")
	}
	for _, f := range []string{"previews.js", "files.js"} {
		src := readStatic(t, f)
		if r, o := strings.Count(src, "state.previewReviews = [];"), strings.Count(src, "state.previewOverviews = [];"); r != o {
			t.Errorf("%s clears the Reviews block %d times and the Overviews block %d times", f, r, o)
		}
	}
	rv := readStatic(t, "reviews.js")
	for _, want := range []string{`<li class="sect">Overviews</li>`, `data-overview=`, `function previewScopeOverviews()`, `"overview:" + `} {
		if !strings.Contains(rv, want) {
			t.Errorf("reviews.js lacks %q", want)
		}
	}
	fs := readStatic(t, "files.js")
	if strings.Count(fs, "li.dataset.overview") < 2 {
		t.Error("files.js does not route a click and a right-click on an Overviews row")
	}
}
