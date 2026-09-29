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
		"└ Review: 09-28 23:37 Claude Code",      // a branch sub-row drops the CURRENT year only
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
