package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

const linkReviewDoc = `{"version":1,"summary":"ok","files":[{"path":"a.txt","annotations":[
 {"newRange":[1,2],"summary":"two lines","rationale":"why"},
 {"oldRange":[1,1],"summary":"removed line"}]}]}`

// reviewLinkFixture is a repo with three commits on a.txt and two reviews:
// one of HEAD's own change, then one of the range HEAD~2..HEAD (saved last,
// so it is "latest").
func reviewLinkFixture(t *testing.T) (svc *Service, dir, single, rng string) {
	t.Helper()
	dir = t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	for i, body := range []string{"a\nb\nc\n", "A\nB\nc\n", "A2\nB2\nC\n"} {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", ".")
		gitRun(t, dir, "commit", "-q", "-m", "c"+string(rune('0'+i)))
	}
	svc = Open(dir)
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	base := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD~2"))
	var err error
	single, _, err = svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Commit: head}, Agent: "Claude", Text: linkReviewDoc})
	if err != nil {
		t.Fatal(err)
	}
	rng, _, err = svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: base + ".." + head, Commit: head}, Agent: "Claude", Text: linkReviewDoc})
	if err != nil {
		t.Fatal(err)
	}
	return svc, dir, single, rng
}

func mustParse(t *testing.T, s string) model.Link {
	t.Helper()
	l, err := model.ParseLink(s)
	if err != nil {
		t.Fatalf("ParseLink(%q): %v", s, err)
	}
	return l
}

// resolveWith resolves through svc itself, so the hint check reads the
// test's own note store (a freshly opened Service would not).
func resolveWith(svc *Service) ResolveOpts {
	return ResolveOpts{Cwd: svc, OpenFn: func(string) *Service { return svc }}
}

func TestReviewLinkSingleAndRange(t *testing.T) {
	t.Parallel()
	svc, dir, single, rng := reviewLinkFixture(t)
	ctx := context.Background()
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	base := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD~2"))
	l1, err := svc.ReviewLink(ctx, single)
	if err != nil || !strings.HasSuffix(l1, "@"+head+"?review="+single) {
		t.Fatalf("single = %q, %v", l1, err)
	}
	l2, err := svc.ReviewLink(ctx, rng)
	if err != nil || !strings.HasSuffix(l2, "@"+base+".."+head+"?review="+rng) {
		t.Fatalf("range = %q, %v", l2, err)
	}
	if id, err := svc.ReviewID(ctx, "latest"); err != nil || id != rng {
		t.Fatalf("latest = %q, %v, want the range review (saved last)", id, err)
	}
	if _, err := svc.ReviewID(ctx, "deadbeef"); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestReviewRemarksCarryTheirOwnLinks(t *testing.T) {
	t.Parallel()
	svc, _, _, rng := reviewLinkFixture(t)
	ctx := context.Background()
	r, err := svc.Review(ctx, rng)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := svc.ReviewRemarks(ctx, r)
	if err != nil || len(rs) != 2 {
		t.Fatalf("remarks = %+v, %v", rs, err)
	}
	if rs[0].N != 0 || rs[0].Start != 1 || rs[0].End != 2 || rs[0].Rationale != "why" ||
		!strings.Contains(rs[0].Link, "/a.txt@") || !strings.HasSuffix(rs[0].Link, ":1-2") {
		t.Fatalf("remark 0 = %+v", rs[0])
	}
	if rs[1].N != 1 || rs[1].Side != model.NoteSideOld || !strings.HasSuffix(rs[1].Link, ":old:1") {
		t.Fatalf("remark 1 = %+v", rs[1])
	}
	for _, x := range rs {
		mustParse(t, x.Link)
	}
}

func TestReviewHintIsChecked(t *testing.T) {
	t.Parallel()
	svc, dir, single, _ := reviewLinkFixture(t)
	ctx := context.Background()
	opts := resolveWith(svc)
	good, err := svc.ReviewLink(ctx, single)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := ResolveLink(ctx, mustParse(t, good), opts); err != nil || res.Hint.Kind != model.ReviewHintKind {
		t.Fatalf("good link: %+v, %v", res, err)
	}
	other := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD~1"))
	bad := good[:strings.LastIndex(good, "@")+1] + other + "?review=" + single
	if _, err := ResolveLink(ctx, mustParse(t, bad), opts); !errors.Is(err, ErrReviewLinkMismatch) {
		t.Fatalf("a link moved to another commit must be refused: %v", err)
	}
	bare := good[:strings.LastIndex(good, "@")] + "?review=" + single
	if _, err := ResolveLink(ctx, mustParse(t, bare), opts); !errors.Is(err, model.ErrLink) {
		t.Fatalf("an address-less review link must be refused: %v", err)
	}
	if err := svc.NoteRemove(ctx, single); err != nil {
		t.Fatal(err)
	}
	if res, err := ResolveLink(ctx, mustParse(t, good), opts); err != nil || res.Hint.ID != single {
		t.Fatalf("a deleted review still resolves its address (consumers notice): %+v, %v", res, err)
	}
}

// A prose review (no document) has no remarks and still links.
func TestProseReviewLinksWithNoRemarks(t *testing.T) {
	t.Parallel()
	svc, dir, _, _ := reviewLinkFixture(t)
	ctx := context.Background()
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	tg := ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Commit: head}
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Agent: "Claude", Text: "# Verdict\nfine"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.Review(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if rs, err := svc.ReviewRemarks(ctx, r); err != nil || rs == nil || len(rs) != 0 {
		t.Fatalf("prose remarks = %#v, %v, want an empty non-nil list", rs, err)
	}
	if l, err := svc.ReviewLink(ctx, id); err != nil || !strings.Contains(l, "?review="+id) {
		t.Fatalf("prose link = %q, %v", l, err)
	}
}

// A copied review link is described by its review in gg links, not as the
// bare commit it addresses.
func TestDescribeReviewLink(t *testing.T) {
	t.Parallel()
	svc, _, single, _ := reviewLinkFixture(t)
	ctx := context.Background()
	text, err := svc.ReviewLink(ctx, single)
	if err != nil {
		t.Fatal(err)
	}
	got := svc.DescribeLink(ctx, mustParse(t, text))
	if !strings.HasPrefix(got, "review: "+single) || !strings.Contains(got, "Claude") {
		t.Fatalf("desc = %q, want review: %s … Claude", got, single)
	}
}

// With only the caller's service (Cwd) — the CLI's, the TUI paste's — the
// check reads THAT service's store: a fresh Open of the same checkout would
// not see it and could not refuse the moved link.
func TestReviewHintCheckUsesTheCallersService(t *testing.T) {
	t.Parallel()
	svc, dir, single, _ := reviewLinkFixture(t)
	ctx := context.Background()
	good, err := svc.ReviewLink(ctx, single)
	if err != nil {
		t.Fatal(err)
	}
	other := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD~1"))
	bad := good[:strings.LastIndex(good, "@")+1] + other + "?review=" + single
	if _, err := ResolveLink(ctx, mustParse(t, bad), ResolveOpts{Cwd: svc}); !errors.Is(err, ErrReviewLinkMismatch) {
		t.Fatalf("Cwd-only resolve must still refuse the moved link: %v", err)
	}
}

// A remark's path is the document's, normalised like every other reader of
// a review document (reviewPath: "./a.txt" → "a.txt").
func TestReviewRemarksNormaliseThePath(t *testing.T) {
	t.Parallel()
	svc, dir, _, _ := reviewLinkFixture(t)
	ctx := context.Background()
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	doc := `{"version":1,"summary":"s","files":[{"path":"./a.txt","annotations":[{"newRange":[1,1],"summary":"x"}]}]}`
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Commit: head}, Agent: "C", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Review(ctx, id)
	rs, err := svc.ReviewRemarks(ctx, r)
	if err != nil || len(rs) != 1 || rs[0].Path != "a.txt" || !strings.Contains(rs[0].Link, "/a.txt@") || strings.Contains(rs[0].Link, "./") {
		t.Fatalf("remark = %+v, %v", rs, err)
	}
}

// ReviewShow's git cost does not grow with the review: the repository and
// the reviewed change are resolved once, not once per remark.
func TestReviewShowGitCostIsFlat(t *testing.T) {
	t.Parallel()
	_, dir, _, _ := reviewLinkFixture(t)
	cr := newCountingRunner(gitexec.NewExecRunner("git", dir, observ.NewRing(50)))
	svc := New(&git.Repo{Runner: cr})
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	var notes []string
	for i := 0; i < 12; i++ {
		notes = append(notes, `{"newRange":[1,1],"summary":"n"}`)
	}
	doc := `{"version":1,"summary":"s","files":[{"path":"a.txt","annotations":[` + strings.Join(notes, ",") + `]}]}`
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Commit: head}, Agent: "C", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	cr.reset()
	rs, err := svc.ReviewShow(ctx, id)
	if err != nil || len(rs.Remarks) != 12 {
		t.Fatalf("show = %d remarks, %v", len(rs.Remarks), err)
	}
	total := 0
	cr.mu.Lock()
	for _, n := range cr.calls {
		total += n
	}
	cr.mu.Unlock()
	if total > 10 {
		t.Fatalf("ReviewShow ran %d git processes for 12 remarks (%v); want a flat cost", total, cr.calls)
	}
}
