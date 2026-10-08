package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestFreshnessSaysUpdatedUntilAQuietRefresh(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	pr := model.PullRequest{Number: 7, HeadSHA: m.previewOpen.srcHash}
	// The open's own first read fills the view: nothing is "updated" yet.
	nm, _ := m.Update(prRevalidatedMsg{n: 7, gen: m.forgeGen, pr: pr, commentsChanged: true})
	m = nm.(Model)
	if got := m.prFreshnessSuffix(); got != "" {
		t.Fatalf("after the first read: %q", got)
	}
	nm, _ = m.Update(prRevalidatedMsg{n: 7, gen: m.forgeGen, pr: pr, commentsChanged: true})
	m = nm.(Model)
	if got := m.prFreshnessSuffix(); !strings.Contains(got, "updated") {
		t.Fatalf("after new comments: %q", got)
	}
	nm, _ = m.Update(prRevalidatedMsg{n: 7, gen: m.forgeGen, pr: pr})
	m = nm.(Model)
	if got := m.prFreshnessSuffix(); got != "" {
		t.Fatalf("after a quiet refresh: %q", got)
	}
}
