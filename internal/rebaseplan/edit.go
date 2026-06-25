package rebaseplan

import (
	"fmt"

	"github.com/homeend/gigagit/internal/model"
)

// Edit is a single-commit history edit applied to a loaded commit range.
type Edit int

const (
	EditDrop Edit = iota
	EditMoveUp
	EditMoveDown
)

// OntoFor returns the rebase base (a git revision expression off the target's
// SHA) for a single-commit edit. Drop and move-up base onto the commit's parent
// (~1); move-down bases onto the grandparent (~2) so the parent is inside the
// rebased range and the commit can swap below it. ~N follows first-parent.
func OntoFor(sha string, e Edit) string {
	if e == EditMoveDown {
		return sha + "~2"
	}
	return sha + "~1"
}

// BuildSingleEdit builds the rebase plan for a single-commit edit over an
// oldest-first commit range (git todo order). move-up swaps the target with the
// next (newer) entry; move-down with the previous (older); drop marks the target
// Drop. Every other entry stays Pick, with Orig carried from the range message.
// Returns an error when the target isn't in the range or the move has no neighbor.
func BuildSingleEdit(commits []model.RangeCommit, target string, e Edit) (Plan, error) {
	entries := make([]Entry, len(commits))
	idx := -1
	for i, c := range commits {
		entries[i] = Entry{Sha: c.Hash, Action: Pick, Orig: c.Message}
		if c.Hash == target {
			idx = i
		}
	}
	if idx == -1 {
		return Plan{}, fmt.Errorf("commit is not on the current branch")
	}
	switch e {
	case EditDrop:
		entries[idx].Action = Drop
	case EditMoveUp:
		if idx+1 >= len(entries) {
			return Plan{}, fmt.Errorf("already the newest commit")
		}
		entries[idx], entries[idx+1] = entries[idx+1], entries[idx]
	case EditMoveDown:
		if idx-1 < 0 {
			return Plan{}, fmt.Errorf("already the oldest commit in range")
		}
		entries[idx], entries[idx-1] = entries[idx-1], entries[idx]
	}
	return Plan{Entries: entries}, nil
}

// BuildDrop builds the rebase plan that drops every target commit over an
// oldest-first commit range (git todo order): each target becomes Drop, every
// other commit keeps Pick with Orig carried from the range message. Unlike
// squash there is no adjacency requirement — non-contiguous drops are valid
// git. Errors when no targets are given or a target isn't in the range.
func BuildDrop(commits []model.RangeCommit, targets []string) (Plan, error) {
	if len(targets) == 0 {
		return Plan{}, fmt.Errorf("select at least 1 commit to drop")
	}
	isTarget := make(map[string]bool, len(targets))
	pos := make(map[string]bool, len(commits))
	for _, c := range commits {
		pos[c.Hash] = true
	}
	for _, t := range targets {
		if !pos[t] {
			return Plan{}, fmt.Errorf("commit %s is not on the current branch", t)
		}
		isTarget[t] = true
	}
	entries := make([]Entry, len(commits))
	for i, c := range commits {
		action := Pick
		if isTarget[c.Hash] {
			action = Drop
		}
		entries[i] = Entry{Sha: c.Hash, Action: action, Orig: c.Message}
	}
	return Plan{Entries: entries}, nil
}
