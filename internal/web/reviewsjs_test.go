package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const reviewsPureStart = "// --- reviews pure (guarded against Go) ---"
const reviewsPureEnd = "// --- end reviews pure ---"

func staticSrc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("static", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// runReviewsPure evaluates reviews.js's pure section, then body (which must
// console.log its answers), under node with TZ=UTC so stamps are fixed.
func runReviewsPure(t *testing.T, body string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src := staticSrc(t, "reviews.js")
	i, j := strings.Index(src, reviewsPureStart), strings.Index(src, reviewsPureEnd)
	if i < 0 || j < i {
		t.Fatalf("reviews.js: the pure section markers are gone")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "t.mjs")
	if err := os.WriteFile(script, []byte(src[i:j]+"\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script)
	cmd.Env = append(os.Environ(), "TZ=UTC")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestReviewRowTexts(t *testing.T) {
	t.Parallel()
	got := runReviewsPure(t, `
const r = { created: "2026-09-29T14:05:09Z", agent: " claude ", summary: "sum" };
console.log(reviewRowText(r));
console.log(branchReviewText(r, new Date("2026-10-01T00:00:00Z")));
console.log(reviewRowText({ created: "", agent: "claude" }));
console.log(reviewRowText({ created: "bad", agent: "", summary: "just the summary" }));
console.log(reviewStamp(""));
const now = new Date("2026-12-01T10:00:00Z");
console.log(branchReviewText({ created: "2026-09-28T23:37:00Z", agent: "Claude Code" }, now));
console.log(branchReviewText({ created: "2025-12-31T08:05:00Z", agent: "Claude Code" }, now));
`)
	want := strings.Join([]string{
		"└ 2026-09-29 14:05 claude",
		"└ Review: 09-29 14:05 claude",
		"└ claude",
		"└ just the summary",
		"",
		"└ Review: 09-28 23:37 Claude Code", // a branch sub-row drops the CURRENT year only
		"└ Review: 2025-12-31 08:05 Claude Code", // …and keeps an older one
	}, "\n")
	if got != strings.TrimSpace(want) {
		t.Errorf("row texts:\n%s\nwant:\n%s", got, want)
	}
}

func TestBranchReviewsMatchCurrentTip(t *testing.T) {
	t.Parallel()
	got := runReviewsPure(t, `
const rs = [
  { id: "a", branch: "main", commit: "abcdef1234567890" },
  { id: "b", branch: "main", commit: "0000000234567890" },
  { id: "c", branch: "dev", commit: "abcdef1234567890" },
];
console.log(branchReviews(rs, { name: "main", hash: "abcdef1" }).map((r) => r.id).join(","));
console.log(branchReviews(rs, { name: "main", hash: "" }).length);
console.log(branchReviews(null, { name: "main", hash: "abcdef1" }).length);
`)
	if got != "a\n0\n0" {
		t.Errorf("branchReviews = %q, want a / 0 / 0", got)
	}
}

func TestReviewOverlayLapsesOnAnotherOpen(t *testing.T) {
	t.Parallel()
	got := runReviewsPure(t, `
const files = [], cmp = {};
console.log([
  reviewActiveIn({ review: { files, cmp: null }, layout: "files", filesMode: "commit", files }),
  reviewActiveIn({ review: { files, cmp: null }, layout: "files", filesMode: "commit", files: [] }),
  reviewActiveIn({ review: { files, cmp: null }, layout: "list", filesMode: "commit", files }),
  reviewActiveIn({ review: { files, cmp }, layout: "diff", filesMode: "compare", compare: cmp }),
  reviewActiveIn({ review: { files, cmp }, layout: "diff", filesMode: "compare", compare: {} }),
  reviewActiveIn({ review: null, layout: "files" }),
].join(","));
`)
	if got != "true,false,false,true,false,false" {
		t.Errorf("got %q", got)
	}
}

// wiringCheck fails on every want missing from the static file.
func wiringCheck(t *testing.T, file string, wants ...string) {
	t.Helper()
	src := staticSrc(t, file)
	for _, w := range wants {
		if !strings.Contains(src, w) {
			t.Errorf("%s lacks %q", file, w)
		}
	}
}

// Review rows are NOT file rows: a row with data-i is walked by the cursor,
// staged, stacked and opened as a file; a branch row with data-n gets the
// branch menu and is a drag source. Neither may carry those.
func TestReviewRowsAreNotFileOrBranchRows(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"files.js", "reviews.js", "sidebar.js"} {
		for _, line := range strings.Split(staticSrc(t, f), "\n") {
			if strings.Contains(line, "data-review=") && (strings.Contains(line, "data-i=") || strings.Contains(line, "data-n=") || strings.Contains(line, "draggable")) {
				t.Errorf("%s: a review row carries a file/branch handle: %s", f, strings.TrimSpace(line))
			}
		}
	}
	wiringCheck(t, "files.js",
		"reviewRowsHTML(", // a commit's reviews head its files
		"if (reviewActive()) return renderReviewFiles();",
	)
	wiringCheck(t, "reviews.js", `<li class="sect">Reviews</li>`, `class="rev`)
	wiringCheck(t, "sidebar.js", `class="brev"`, "branchReviews(", "branchReviewText(")
}

// The review view's note lane is the review's and ONLY the review's, and no
// write reaches it: `c` in a review would otherwise file a real note against
// the commit, next to notes the reader cannot tell apart.
func TestReviewNoteLaneIsExclusiveAndReadOnly(t *testing.T) {
	t.Parallel()
	wiringCheck(t, "files.js",
		"if (ctx.review) {",       // noteQuery's review arm
		`"/api/review/notes?"`,    // notesFor's endpoint for it
		"ad.ctx && ad.ctx.review", // addNotePrompt refuses
		"a review's notes are read-only",
		"review: state.review.id", // commitDiffCtx carries the lane
	)
	src := staticSrc(t, "files.js")
	i := strings.Index(src, "function rowNoteCtx(")
	j := strings.Index(src[i:], "state.compare.links")
	k := strings.Index(src[i:], "reviewActive()")
	if i < 0 || k < 0 || k > j {
		t.Error("rowNoteCtx must consult the review overlay before the compare arms (a range review is a compare)")
	}
}

// Opening, leaving and deleting a review.
func TestReviewViewWiring(t *testing.T) {
	t.Parallel()
	wiringCheck(t, "reviews.js",
		"++state.detailGen",   // a newer open or esc supersedes a slow one
		"Opening the review…", // the loading state
		`"/api/review/" + encodeURIComponent(id)`,
		"the review is gone",
		`"/api/notes/remove"`,
		`["cancel", "delete"]`, // cancel first; esc answers it
		"Delete review",
		"copyText(", // the Overview's Copy
	)
	wiringCheck(t, "files.js",
		"leaveReview()",     // esc from a review's file list
		"li.dataset.review", // the click/contextmenu routes
		"showReviewOverview()",
	)
	wiringCheck(t, "files.js", "reviews: c.reviews || []", "renderBranches()",
		"cr.list = state.noteCounts.reviews.filter(", // the open commit's rows follow the counts
	)
	wiringCheck(t, "commits.js", "state.commitReviews = { sha:")
	// The keyboard reaches a commit's Reviews rows: k above the first file,
	// enter opens the selected one.
	wiringCheck(t, "keys.js", "stepCommitReviews(delta)", "openSelectedReview()")
}

// Stacked, a review's Overview is the first element of the stack (the TUI's
// stacked review view), and toggling the stack on the Overview keeps it.
func TestReviewOverviewTopsTheStack(t *testing.T) {
	t.Parallel()
	wiringCheck(t, "stackview.js",
		`const ov = reviewActive() ? `+"`"+`<div class="stk-ov">${reviewOverviewHTML()}</div>`+"`"+` : "";`,
		"if (reviewActive() && state.review.onOverview) return showReviewOverview();",
		"state.review.onOverview = top;",
	)
	wiringCheck(t, "reviews.js", "return openStack(0); // buildStack lands on the Overview")
	wiringCheck(t, "style.css", ".review-ov { padding: 12px 16px; max-width: 110ch; margin: 0 auto; }")
}

// A reviewed commit carries ✎ in the commit list, from the note counts the
// page already holds (no read per row), and the list repaints when they change.
func TestCommitRowsMarkReviewed(t *testing.T) {
	t.Parallel()
	wiringCheck(t, "commits.js", "reviewedHashes().has(row.hash)", `class="rvmark"`)
	wiringCheck(t, "files.js", "renderCommits(); // the ✎ on reviewed commits")
}

// `,` / `.` on a review's file list step to the previous / next file the
// review notes, from the Overview (-1) too, and stay put at the ends (the
// TUI's stepReviewFile).
func TestNextNotedFile(t *testing.T) {
	t.Parallel()
	got := runReviewsPure(t, `
const fs = [{ path: "a" }, { path: "b" }, { path: "c" }, { path: "d" }];
const counts = { b: 2, d: 1 };
console.log([
  nextNotedFile(fs, counts, -1, 1), nextNotedFile(fs, counts, 1, 1), nextNotedFile(fs, counts, 3, 1),
  nextNotedFile(fs, counts, 3, -1), nextNotedFile(fs, counts, 1, -1), nextNotedFile(fs, {}, -1, 1),
].join(","));`)
	if got != "1,3,-1,1,-1,-1" {
		t.Fatalf("nextNotedFile = %s, want 1,3,-1,1,-1,-1", got)
	}
}

// The keys route `,` / `.` to the noted-file step on a review's file list,
// before the diff's change-step arm.
func TestReviewFileStepKeyWiring(t *testing.T) {
	t.Parallel()
	keys := staticSrc(t, "keys.js")
	i := strings.Index(keys, "stepReviewFile(e.key === \".\" ? 1 : -1)")
	j := strings.Index(keys, "stepChange(e.key === \".\" ? 1 : -1)")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("keys.js: the review file step must come before the change step (%d, %d)", i, j)
	}
	if !strings.Contains(keys[:i], `reviewActive() && state.layout === "files"`) {
		t.Fatal("keys.js: the review file step is not gated on a review's file list")
	}
}

// A commit row earns the ✎ for an AI review and for a range review of either
// form — a merge preview ("branch review") or a commit pair — and for nothing
// else: plain notes are not a review.
func TestReviewMarkTitle(t *testing.T) {
	t.Parallel()
	got := runReviewsPure(t, `
const scopes = {
  tip: [{ scope: "main...feature", label: "feature → main", n: 3 }],
  mid: [{ scope: "aaaaaaa..bbbbbbb", label: "aaaaaaa..bbbbbbb", n: 1 }],
  none: [],
};
console.log(reviewMarkTitle("tip", false, scopes));
console.log(reviewMarkTitle("mid", false, scopes));
console.log(reviewMarkTitle("mid", true, scopes));
console.log(reviewMarkTitle("ai", true, scopes));
console.log("[" + reviewMarkTitle("none", false, scopes) + "]");
console.log("[" + reviewMarkTitle("plain", false, scopes) + "]");
console.log("[" + reviewMarkTitle("plain", false, undefined) + "]");
`)
	want := strings.Join([]string{
		"has a range review — open the commit to read it",
		"has a range review — open the commit to read it",
		"has an AI review and a range review — open the commit to read them",
		"has an AI review — open the commit to read it",
		"[]", "[]", "[]",
	}, "\n")
	if got != want {
		t.Errorf("mark titles:\n%s\nwant:\n%s", got, want)
	}
}

// The commit row draws its ✎ from reviewMarkTitle and from nothing else: a
// second, AI-review-only test beside it would drop the range review's mark.
func TestCommitRowMarkGoesThroughReviewMarkTitle(t *testing.T) {
	t.Parallel()
	src := staticSrc(t, "commits.js")
	if !strings.Contains(src, "reviewMarkTitle(row.hash, reviewedHashes().has(row.hash), state.noteCounts && state.noteCounts.scopes_by_commit)") {
		t.Fatal("commits.js: the row's ✎ must be decided by reviewMarkTitle over the reviews and scopes_by_commit")
	}
	if n := strings.Count(src, `class="rvmark"`); n != 1 {
		t.Fatalf("commits.js: %d rvmark sites, want the one in rowHTML", n)
	}
}

// A commit's Notes rows: the paths with plain notes at the commit that it
// does not change — a changed file badges its own row, another commit's notes
// never count, and the list is sorted.
func TestNotedElsewherePaths(t *testing.T) {
	t.Parallel()
	got := runReviewsPure(t, `
const plain = { "abc:z/y.txt": 2, "abc:a.txt": 1, "abc:c.txt": 1, "abc:gone.txt": 0, "def:b.txt": 1 };
console.log(JSON.stringify(notedElsewherePaths(plain, "abc", [{ path: "c.txt" }])));
console.log(JSON.stringify(notedElsewherePaths(plain, "def", [{ path: "b.txt" }])));
console.log(JSON.stringify(notedElsewherePaths(undefined, "abc", [])));
console.log(JSON.stringify(notedElsewherePaths(plain, "", [])));
`)
	want := "[\"a.txt\",\"z/y.txt\"]\n[]\n[]\n[]"
	if got != want {
		t.Errorf("noted paths:\n%s\nwant:\n%s", got, want)
	}
}
