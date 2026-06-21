package domain

import (
	"context"
	"slices"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
)

func TestDateOrderPagerUsesDateOrder(t *testing.T) {
	f := gitexec.NewFakeRunner()
	var argv []string
	f.SetHandler("git log", func(ctx context.Context, a []string) (gitexec.Result, error) {
		argv = a
		return gitexec.Result{Stdout: ""}, nil
	})
	svc := New(&git.Repo{Runner: f})
	p := dateOrderPager{svc: svc}

	if p.Name() != "date-order" {
		t.Errorf("Name() = %q, want date-order", p.Name())
	}
	if _, err := p.Page(context.Background(), 10, 0, 1, LogScope{}); err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !slices.Contains(argv, "--date-order") {
		t.Errorf("git log argv missing --date-order: %v", argv)
	}
}
