package domain

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
)

// unfetchedPRResolve resolves link on a checkout holding main and no
// refs/gg/pr/*, with or without the navigation-only option.
func unfetchedPRResolve(t *testing.T, link string, unfetched bool) (Resolved, error) {
	t.Helper()
	dir := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(state, dir, "gigagit", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	return ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, UnfetchedPR: unfetched})
}

// Follow-ups 4: a link to a PR this repo has not fetched opens for
// NAVIGATION — the landing fetches it — instead of being refused before any
// frontend sees it. Without the option (review, MCP, compare) it is refused
// as before.
func TestAnUnfetchedPRLinkResolvesForNavigation(t *testing.T) {
	t.Parallel()
	const link = "gg://gigagit/README.md@main...refs/gg/pr/7:1"
	got, err := unfetchedPRResolve(t, link, true)
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if got.Preview == nil || got.Preview.Source != "refs/gg/pr/7" || got.Preview.Target != "main" {
		t.Fatalf("Preview = %+v", got.Preview)
	}
	if got.Addr.Path != "README.md" || got.Line != 1 {
		t.Errorf("Addr = %+v line %d", got.Addr, got.Line)
	}
	if _, err := unfetchedPRResolve(t, link, false); !errors.Is(err, ErrLinkUnknownRepo) || !strings.Contains(err.Error(), "gg pr fetch 7") {
		t.Fatalf("without the option: err = %v", err)
	}
}

// The base half still has to be here: the PR's own fetch needs nothing
// else, but a checkout without the base cannot show the PR.
func TestAnUnfetchedPRLinkStillNeedsTheBase(t *testing.T) {
	t.Parallel()
	if _, err := unfetchedPRResolve(t, "gg://gigagit@nosuch...refs/gg/pr/7", true); !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v", err)
	}
}

// Only a PR ref is relaxed: a missing branch is still a missing branch.
func TestAPlainPreviewLinkIsNotRelaxed(t *testing.T) {
	t.Parallel()
	if _, err := unfetchedPRResolve(t, "gg://gigagit@main...feat/missing", true); !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v", err)
	}
}

// Final review minor 2: with two checkouts of the repo, the one that has
// fetched the PR wins over one that has not (it shows the PR without a
// second fetch) — even when the other is the more recently opened.
func TestAnUnfetchedPRLinkPrefersACheckoutHoldingThePR(t *testing.T) {
	t.Parallel()
	with := linkRepoWithBranch(t, "gigagit", "feat/x")
	runGitIn(t, with, "update-ref", "refs/gg/pr/7", "feat/x") // fetched here, ahead of main
	without := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(state, with, "gigagit", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := repos.Touch(state, without, "gigagit", time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	l, _ := model.ParseLink("gg://gigagit@main...refs/gg/pr/7")
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, UnfetchedPR: true})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, with) {
		t.Fatalf("Checkout = %q, want the one holding the PR %q", got.Checkout, with)
	}
}

// Final review minor 4: a navigation needs only the base of a PR link, so
// its refusal names the base alone.
func TestAnUnfetchedPRLinkRefusalNamesTheBase(t *testing.T) {
	t.Parallel()
	_, err := unfetchedPRResolve(t, "gg://gigagit@nosuch...refs/gg/pr/7", true)
	if err == nil || strings.Contains(err.Error(), "holds both") || !strings.Contains(err.Error(), "nosuch") {
		t.Fatalf("err = %v", err)
	}
}

// Follow-ups 5, item 1: `gg pr forget` deletes the PR's ref but not its
// objects, so a review saved on the PR still resolves its tip — the link's
// unfetched landing has no commit yet and must navigate (the landing
// fetches), not be refused as "review … compared …".
func TestAReviewLinkToAnUnfetchedPRNavigates(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	ctx := context.Background()
	rid, _, err := svc.SaveReview(ctx, SaveReview{Target: ScopeReviewTarget(prNoteSetOf(t, svc)), // a review saved on the PR
		Agent: "claude", Text: twoRemarks})
	if err != nil {
		t.Fatal(err)
	}
	runGitIn(t, repoDir(t, svc), "update-ref", "-d", "refs/gg/pr/7") // gg pr forget
	l, err := model.ParseLink("gg://" + localLinkRoot(t, svc) + "/big.go@main...refs/gg/pr/7:5?review=" + rid)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc, UnfetchedPR: true})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if got.Preview == nil || got.Preview.Tip != "" || got.Hint.ID != rid {
		t.Fatalf("Preview = %+v hint %+v", got.Preview, got.Hint)
	}
}

// Follow-ups 5, item 2: an inspection (review, compare, gg link resolve)
// still refuses an unfetched PR's link — it reads the PR's commits — but the
// refusal says how to fix it instead of naming a ref the user never typed.
func TestAnUnfetchedPRLinkInspectionSaysFetchIt(t *testing.T) {
	t.Parallel()
	_, err := unfetchedPRResolve(t, "gg://gigagit@main...refs/gg/pr/7", false)
	if !errors.Is(err, ErrLinkUnknownRepo) || !strings.Contains(err.Error(), "pull request #7 is not fetched here") ||
		!strings.Contains(err.Error(), "gg pr fetch 7") {
		t.Fatalf("err = %v", err)
	}
	// Without the base the PR is not the problem: the old refusal stands.
	if _, err := unfetchedPRResolve(t, "gg://gigagit@nosuch...refs/gg/pr/7", false); err == nil || strings.Contains(err.Error(), "gg pr fetch") {
		t.Fatalf("no base: err = %v", err)
	}
}
