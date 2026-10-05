package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// sourceMsgFrom runs cmd (batches flattened, nested ones too) and returns the
// dataAvailableMsg it produced for source s.
func sourceMsgFrom(t *testing.T, cmd tea.Cmd, s sourceKey) (dataAvailableMsg, bool) {
	t.Helper()
	var walk func(tea.Cmd) (dataAvailableMsg, bool)
	walk = func(c tea.Cmd) (dataAvailableMsg, bool) {
		if c == nil {
			return dataAvailableMsg{}, false
		}
		switch v := c().(type) {
		case tea.BatchMsg:
			for _, sub := range v {
				if da, ok := walk(sub); ok {
					return da, true
				}
			}
		case dataAvailableMsg:
			if v.source == s {
				return v, true
			}
		}
		return dataAvailableMsg{}, false
	}
	return walk(cmd)
}

// A repo switch reads the NEW repo's note counts — the review ✎ / ◆ markers,
// a commit's Reviews rows, the file ◆ badges — and never shows the old
// repo's in the meantime. Before, only `r` re-read them (user report: icons
// missing after switching repos until a refresh).
func TestReRootReadsTheNewReposNoteCounts(t *testing.T) {
	t.Parallel()
	dirA, dirB := navRepo(t), navRepo(t)
	notesB := t.TempDir()
	svcB := domain.Open(dirB)
	svcB.UseNotesDir(notesB)
	ctx := context.Background()
	head, _, err := svcB.ResolveRev(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	head = strings.TrimSpace(head)
	id, _, err := svcB.SaveReview(ctx, domain.SaveReview{Target: domain.ReviewTarget{Kind: domain.ReviewRange, Range: head + "^.." + head, Commit: head}, Agent: "Claude", Text: "# ok"})
	if err != nil {
		t.Fatal(err)
	}

	m := New(domain.Open(dirA))
	m.width, m.height = 160, 40
	m.noteCounts.Reviews = []domain.ReviewHead{{ID: "stale-from-A", Commit: head}}

	updated, _ := m.reRoot(dirB)
	m = updated.(Model)
	if len(m.noteCounts.Reviews) != 0 {
		t.Fatalf("the old repo's note counts survived the switch: %+v", m.noteCounts.Reviews)
	}
	m.svc.UseNotesDir(notesB) // the new Service reads B's store

	updated, cmd := m.Update(m.loadCmd()())
	m = updated.(Model)
	da, ok := sourceMsgFrom(t, cmd, srcNotes)
	if !ok {
		t.Fatal("the switched-to repo's snapshot must chain a note-counts read")
	}
	updated, _ = m.Update(da)
	m = updated.(Model)
	if len(m.noteCounts.Reviews) != 1 || m.noteCounts.Reviews[0].ID != id {
		t.Fatalf("note counts after the switch = %+v, want B's review %s", m.noteCounts.Reviews, id)
	}
	if m.loading || !m.ready {
		t.Fatalf("the chained read must not hold the gate: loading=%v ready=%v", m.loading, m.ready)
	}
}
