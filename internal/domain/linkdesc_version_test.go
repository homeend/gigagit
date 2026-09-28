package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// A version link describes as the record when this store holds it, and falls
// through to the pair's own description when it does not.
func TestDescribeLinkVersionHitAndMiss(t *testing.T) {
	t.Parallel()
	svc, c1, c2, c3 := findVersionFixture(t)
	ctx := context.Background()
	hit, err := model.ParseLink("gg://r@" + c1 + ".." + c3 + "?version=1700001000-pull")
	if err != nil {
		t.Fatal(err)
	}
	// The date is rendered in LOCAL time, like the versions popup's rows.
	want := "version: main · pull · " + time.Unix(1700001000, 0).Format("2006-01-02 15:04")
	if got := svc.DescribeLink(ctx, hit); got != want {
		t.Fatalf("hit: %q, want %q", got, want)
	}
	miss, err := model.ParseLink("gg://r@" + c2 + ".." + c3 + "?version=1600000000-rebase")
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.DescribeLink(ctx, miss); !strings.HasPrefix(got, "link: gg://r@"+c2) {
		t.Fatalf("miss: %q, want the pair fall-through", got)
	}
}

// An address-less ?version= names nothing: the record is not the link's
// content, the pair is.
func TestResolveLinkRefusesAnAddresslessVersionHint(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := findVersionFixture(t)
	l, err := model.ParseLink("gg://" + localLinkRoot(t, svc) + "?version=1700001000-pull")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if !errors.Is(err, model.ErrLink) || !strings.Contains(err.Error(), "a version hint needs the preview it names") {
		t.Fatalf("err = %v, want the version-specific refusal", err)
	}
}
