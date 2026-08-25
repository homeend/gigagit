package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func st(files ...model.FileStatus) model.WorkingTreeStatus {
	return model.WorkingTreeStatus{Files: files}
}

func TestDeriveWipRows(t *testing.T) {
	t.Parallel()
	unstaged := model.FileStatus{Path: "a", Unstaged: 'M'}
	staged := model.FileStatus{Path: "b", Staged: 'M'}
	both := model.FileStatus{Path: "c", Staged: 'M', Unstaged: 'M'}

	cases := []struct {
		name string
		in   model.WorkingTreeStatus
		want []wipRow
	}{
		{"clean", st(), nil},
		{"only unstaged", st(unstaged), []wipRow{{wipWorktree, 1}}},
		{"only staged", st(staged), []wipRow{{wipStaged, 1}}},
		{"both via one file", st(both), []wipRow{{wipWorktree, 1}, {wipStaged, 1}}},
		{"both via two files", st(unstaged, staged), []wipRow{{wipWorktree, 1}, {wipStaged, 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deriveWipRows(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("row %d: got %v, want %v", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestWipAccessors(t *testing.T) {
	t.Parallel()
	m := Model{
		wipRows: []wipRow{{wipWorktree, 2}, {wipStaged, 1}},
		commits: []model.Commit{{Hash: "h0"}, {Hash: "h1"}},
	}
	if m.wipCount() != 2 || m.commitsTotal() != 4 {
		t.Fatalf("wipCount=%d total=%d", m.wipCount(), m.commitsTotal())
	}
	if !m.isWipRow(0) || !m.isWipRow(1) || m.isWipRow(2) {
		t.Fatal("isWipRow boundary wrong")
	}
	if r, ok := m.wipRowAt(1); !ok || r.kind != wipStaged {
		t.Fatalf("wipRowAt(1) = %v,%v", r, ok)
	}
	if _, ok := m.wipRowAt(2); ok {
		t.Fatal("wipRowAt past wip range must be false")
	}
}

// TestWipEndpointsOrder verifies wipEndpoints returns (left=older, right=newer)
// — the ordering DiffTreeFiles requires (internal/git/compare.go's four
// supported pairs: Commit→Commit, Commit→Index, Commit→WorkTree, Index→
// WorkTree). A reversed pair falls through to DiffTreeFiles' "unsupported
// endpoint pair" error, which is the l/enter-on-WIP-row bug.
func TestWipEndpointsOrder(t *testing.T) {
	t.Parallel()
	head := model.Endpoint{Kind: model.EndpointCommit, Hash: "h0"}

	cases := []struct {
		name    string
		wipRows []wipRow
		row     wipRow
		left    model.Endpoint
		right   model.Endpoint
	}{
		{
			name:    "staged row compares HEAD to index",
			wipRows: []wipRow{{wipStaged, 1}},
			row:     wipRow{wipStaged, 1},
			left:    head,
			right:   model.Endpoint{Kind: model.EndpointIndex},
		},
		{
			name:    "worktree row with staged present compares index to worktree",
			wipRows: []wipRow{{wipWorktree, 1}, {wipStaged, 1}},
			row:     wipRow{wipWorktree, 1},
			left:    model.Endpoint{Kind: model.EndpointIndex},
			right:   model.Endpoint{Kind: model.EndpointWorkTree},
		},
		{
			name:    "worktree row with nothing staged compares HEAD to worktree",
			wipRows: []wipRow{{wipWorktree, 1}},
			row:     wipRow{wipWorktree, 1},
			left:    head,
			right:   model.Endpoint{Kind: model.EndpointWorkTree},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Model{wipRows: c.wipRows, commits: []model.Commit{{Hash: "h0"}}}
			left, right := m.wipEndpoints(c.row)
			if left != c.left || right != c.right {
				t.Fatalf("wipEndpoints(%v) = (%v, %v), want (%v, %v)", c.row, left, right, c.left, c.right)
			}
		})
	}
}
