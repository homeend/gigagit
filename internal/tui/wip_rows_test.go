package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func wipStatus(files ...model.FileStatus) model.WorkingTreeStatus {
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
		{"clean", wipStatus(), nil},
		{"only unstaged", wipStatus(unstaged), []wipRow{{wipWorktree, 1}}},
		{"only staged", wipStatus(staged), []wipRow{{wipStaged, 1}}},
		{"both via one file", wipStatus(both), []wipRow{{wipWorktree, 1}, {wipStaged, 1}}},
		{"both via two files", wipStatus(unstaged, staged), []wipRow{{wipWorktree, 1}, {wipStaged, 1}}},
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
	head := mustCommitEndpoint("1234567")

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
			right:   model.IndexEndpoint(),
		},
		{
			name:    "worktree row with staged present compares index to worktree",
			wipRows: []wipRow{{wipWorktree, 1}, {wipStaged, 1}},
			row:     wipRow{wipWorktree, 1},
			left:    model.IndexEndpoint(),
			right:   model.WorkTreeEndpoint(),
		},
		{
			name:    "worktree row with nothing staged compares HEAD to worktree",
			wipRows: []wipRow{{wipWorktree, 1}},
			row:     wipRow{wipWorktree, 1},
			left:    head,
			right:   model.WorkTreeEndpoint(),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Model{wipRows: c.wipRows, commits: []model.Commit{{Hash: "1234567"}}}
			left, right, ok := m.wipEndpoints(c.row)
			if !ok {
				t.Fatalf("wipEndpoints(%v) ok = false, want true (a commit exists)", c.row)
			}
			if left != c.left || right != c.right {
				t.Fatalf("wipEndpoints(%v) = (%v, %v), want (%v, %v)", c.row, left, right, c.left, c.right)
			}
		})
	}
}

// TestWipEndpointsZeroCommitsDeclinesRatherThanInvalidEndpoint covers the
// crash the review found: in a zero-commit repo, a Staged (or a Working tree
// row with nothing staged) pair would need HEAD, which does not exist yet.
// wipEndpoints must report ok=false rather than handing back an
// EndpointInvalid left side — openCompareFiles calls left.CacheTag() and
// left.Display() synchronously, before any git call, and both panic on an
// unset Endpoint. A Working tree row WITH something staged never touches
// HEAD (index ↔ working tree), so it stays valid even with zero commits.
func TestWipEndpointsZeroCommitsDeclinesRatherThanInvalidEndpoint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		wipRows []wipRow
		row     wipRow
		wantOK  bool
	}{
		{"staged row with zero commits declines", []wipRow{{wipStaged, 1}}, wipRow{wipStaged, 1}, false},
		{"worktree row with nothing staged and zero commits declines", []wipRow{{wipWorktree, 1}}, wipRow{wipWorktree, 1}, false},
		{"worktree row with staged present stays valid even with zero commits",
			[]wipRow{{wipWorktree, 1}, {wipStaged, 1}}, wipRow{wipWorktree, 1}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Model{wipRows: c.wipRows, commits: nil}
			left, right, ok := m.wipEndpoints(c.row)
			if ok != c.wantOK {
				t.Fatalf("wipEndpoints(%v) ok = %v, want %v (left=%+v right=%+v)", c.row, ok, c.wantOK, left, right)
			}
		})
	}
}

// TestWipRowEnterZeroCommitsDoesNotPanic drives the real call path the
// review reproduced the crash through: a zero-commit Model with a staged WIP
// row, enter on the Commits panel. Before the fix this panicked inside
// openCompareFiles (left.CacheTag()/left.Display() on an EndpointInvalid
// left side, called synchronously before any git call) and cmd/gg/main.go's
// recover only dumps and re-panics, crashing the process. It must instead
// stay up and leave a status notice, per this repo's "never trap the user"
// convention.
func TestWipRowEnterZeroCommitsDoesNotPanic(t *testing.T) {
	t.Parallel()
	m := Model{
		width:   120,
		height:  40,
		focus:   panelCommits,
		commits: nil,
		wipRows: []wipRow{{wipStaged, 1}},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("enter on a zero-commit Staged row must not panic: %v", r)
		}
	}()
	mm, _ := m.Update(keyMsg("enter"))
	got := mm.(Model)
	if got.filesView != nil {
		t.Error("a zero-commit Staged row has no HEAD to compare against; the files view must not open")
	}
	if got.statusMsg == "" {
		t.Error("declining must leave a status notice, not a silent no-op")
	}
}
