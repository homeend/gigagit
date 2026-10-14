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
  [...overviewCommits(ovs)].sort().join("+"),
  overviewIDsChanged(undefined, ["o1"]), overviewIDsChanged(["o1", "o2"], ["o2", "o1"]),
  overviewIDsChanged(["o1"], ["o1", "o3"]), overviewIDsChanged(["o1", "o2"], ["o1", "o3"]),
].join("|"));
`)
	if want := "o2+o1|0|o2+o1|0|true|abc1234def+fff0000aaa|false|false|true|true"; out != want {
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

// Final review fixes (plan 3).
func TestOverviewReviewFixesAreWired(t *testing.T) {
	t.Parallel()
	a := readStatic(t, "allnotes.js")
	// 1: the viewer and the popup share a z-index — the popup steps aside and
	// comes back when the overview closes (the review row's way).
	if !strings.Contains(a, `hide();
      openPermanentOverview(r.overview.id, { onClose: reshow });`) {
		t.Error("allnotes.js does not hide the popup and reshow it when the overview closes")
	}
	v := readStatic(t, "viewer.js")
	for _, want := range []string{`export async function openPermanentOverview(id, { onClose = null, doc = null } = {})`, `const after = afterClose;`} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	// 2: overview rows take the note columns' layout.
	for _, want := range []string{`#allnotes-list li.an-review, #allnotes-list li.an-overview { display: flex; white-space: nowrap; }`,
		`#allnotes-list li.an-review, #allnotes-list li.an-overview { padding-left: calc(10px + 10ch); }`} {
		if !strings.Contains(a, want) {
			t.Errorf("allnotes.js lacks %q", want)
		}
	}
	// 3: ovTitle adds "Overview: " — the opener passes the bare title.
	if strings.Contains(v, `title: "Overview: " + d.title`) {
		t.Error("viewer.js doubles the Overview: prefix")
	}
	// 4: a delete takes the PR view's row away at once.
	if !strings.Contains(readStatic(t, "overviews.js"), `state.previewOverviews = (state.previewOverviews || []).filter((x) => x.id !== id);`) {
		t.Error("overviews.js does not drop a deleted overview from the PR view's block")
	}
}

// The leftovers branch: every overview row menu exports; one delete at a
// time; the narrow layout's way back goes with a deleted overview; a
// steered link reads the document once; the PR view's block follows a
// save or delete behind the page.
func TestOverviewLeftoversAreWired(t *testing.T) {
	t.Parallel()
	for file, wants := range map[string][]string{
		"overviews.js": {`{ label: "Export as HTML", act: () => exportPermanentOverview(id) }`, `runOnce("overview-delete", () => removeOverview(id, done))`},
		"commits.js":   {`"export as HTML: " + o.title`, `overviewedHashes().has(row.hash)`},
		"allnotes.js":  {`overviewMenu(r.overview.id, e.clientX, e.clientY, { open: () => activate(i)`},
		"viewer.js": {`export function exportPermanentOverview(id)`, `if (!goneStoredOverview(view.from, ids)) return;`,
			`"a permanent overview's anchor has no reference — View all notes copies its link (ctrl+l)"`},
		"live.js":  {`await openPermanentOverview(s.hint_id, { doc });`},
		"files.js": {`if (po && po.pr && overviewIDsChanged(idsBefore, state.noteCounts.overview_ids)) loadPRCounts(po.pr);`},
	} {
		src := readStatic(t, file)
		for _, w := range wants {
			if !strings.Contains(src, w) {
				t.Errorf("%s lacks %q", file, w)
			}
		}
	}
	if strings.Count(readStatic(t, "live.js"), `getJSON("/api/overview/" + encodeURIComponent(s.hint_id))`) != 1 {
		t.Error("live.js reads a steered overview more than once")
	}
}
