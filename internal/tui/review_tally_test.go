package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/i18n"
)

func TestReviewTallyWords(t *testing.T) {
	t.Parallel()
	cases := []struct {
		remarks, resolved, room int
		want                    string
	}{
		{0, 0, 80, ""},
		{12, 4, 80, i18n.T("%d remarks · %d resolved", 12, 4)},
		{1, 0, 80, i18n.T("1 remark · %d resolved", 0)},
		{12, 4, 10, i18n.T("%d/%d resolved", 4, 12)},
	}
	for _, c := range cases {
		if got := reviewTally(c.remarks, c.resolved, c.room); got != c.want {
			t.Errorf("reviewTally(%d,%d,%d) = %q, want %q", c.remarks, c.resolved, c.room, got, c.want)
		}
	}
	for _, g := range []string{"✓", "✗", "✔"} {
		if strings.Contains(reviewTally(5, 2, 80), g) {
			t.Fatalf("a tally must be plain words (user ruling): %q", reviewTally(5, 2, 80))
		}
	}
	if got := withTally("└ 2026-10-05 Claude", 3, 1, 80); got != "└ 2026-10-05 Claude · "+i18n.T("%d remarks · %d resolved", 3, 1) {
		t.Fatalf("withTally = %q", got)
	}
	if got := withTally("└ x", 0, 0, 80); got != "└ x" {
		t.Fatalf("no remarks, no tally: %q", got)
	}
}
