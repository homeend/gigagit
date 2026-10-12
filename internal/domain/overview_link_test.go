package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestOverviewLinkRoundTrip(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	ctx := context.Background()
	got, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	text, err := svc.OverviewLink(ctx, got.ID)
	if err != nil || !strings.HasSuffix(text, "?overview="+got.ID) || !strings.Contains(text, revParse(t, dir, "HEAD")) {
		t.Fatalf("link = %q %v", text, err)
	}
	l, err := model.ParseLink(text)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc})
	if err != nil || res.Hint.Kind != model.OverviewHintKind || res.Hint.ID != got.ID {
		t.Fatalf("resolve = %+v %v", res.Hint, err)
	}
	// moved onto another commit: refused
	commitFile(t, dir, "c.go", "c\n", "next")
	l.Target.Commit = revParse(t, dir, "HEAD")
	if _, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc}); !errors.Is(err, ErrOverviewLinkMismatch) {
		t.Fatalf("moved link: %v", err)
	}
	// gone: refused
	if err := svc.RemoveOverview(ctx, got.ID); err != nil {
		t.Fatal(err)
	}
	l, _ = model.ParseLink(text)
	if _, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc}); !errors.Is(err, ErrOverviewNotFound) {
		t.Fatalf("gone: %v", err)
	}
}
