package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

// Task 10: PR 7's link pair is its base against gg's private head ref; a PR
// that was never fetched has no link (it would not resolve).
func TestPRLinkPair(t *testing.T) {
	t.Parallel()
	svc, ff, _ := sendRepo(t)
	p, err := svc.PRLinkPair(context.Background(), 7)
	if err != nil || p.Head != git.PRRef(7) || p.Base != "main" {
		t.Fatalf("pair %+v err %v", p, err)
	}
	pr8 := ff.byNum[7]
	pr8.Number = 8
	ff.mu.Lock()
	ff.byNum[8] = pr8
	ff.mu.Unlock()
	if _, err := svc.PRLinkPair(context.Background(), 8); !errors.Is(err, ErrPRNotFetched) {
		t.Fatalf("unfetched: %v", err)
	}
}
