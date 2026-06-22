package rebaseplan

import (
	"errors"
	"fmt"
	"sort"

	"github.com/gigagit/gg/internal/model"
)

// ErrNotAdjacent is returned by BuildSquash when the selected commits have gaps
// (an unselected commit lies between the oldest and newest target). Callers
// (Stage 2) detect it with errors.Is to offer reorder-then-squash.
var ErrNotAdjacent = errors.New("selected commits are not adjacent")

// squashTargets validates targets against the oldest-first range and returns the
// unique target indices in range order plus an isTarget set. It errors on fewer
// than 2 distinct targets or a target not present in the range.
func squashTargets(commits []model.RangeCommit, targets []string) (idxs []int, isTarget map[string]bool, err error) {
	pos := make(map[string]int, len(commits))
	for i, c := range commits {
		pos[c.Hash] = i
	}
	isTarget = make(map[string]bool, len(targets))
	for _, t := range targets {
		i, ok := pos[t]
		if !ok {
			return nil, nil, fmt.Errorf("commit %s is not on the current branch", shortSquashSHA(t))
		}
		if !isTarget[t] {
			isTarget[t] = true
			idxs = append(idxs, i)
		}
	}
	if len(idxs) < 2 {
		return nil, nil, fmt.Errorf("select at least 2 commits to squash")
	}
	sort.Ints(idxs)
	return idxs, isTarget, nil
}

// BuildSquash builds the rebase plan that squashes the target commits into one,
// over an oldest-first commit range (git todo order). The oldest target (the
// smallest range index) stays Pick; every other target becomes Squash; all
// other commits keep Pick, with Orig carried from the range message. The targets
// must be adjacent in the range; gaps return ErrNotAdjacent (Stage 2 offers to
// reorder them adjacent via BuildSquashReorder).
//
// Errors when fewer than 2 targets are given, a target is not in the range, or
// the targets are not adjacent (ErrNotAdjacent).
func BuildSquash(commits []model.RangeCommit, targets []string) (Plan, error) {
	idxs, isTarget, err := squashTargets(commits, targets)
	if err != nil {
		return Plan{}, err
	}
	lo, hi := idxs[0], idxs[len(idxs)-1]
	if hi-lo+1 != len(idxs) {
		return Plan{}, ErrNotAdjacent
	}
	entries := make([]Entry, len(commits))
	for i, c := range commits {
		action := Pick
		if isTarget[c.Hash] && i != lo {
			action = Squash
		}
		entries[i] = Entry{Sha: c.Hash, Action: action, Orig: c.Message}
	}
	return Plan{Entries: entries}, nil
}

// BuildSquashReorder builds a plan that first replays the target commits
// consecutively (oldest = Pick, the rest Squash) and then every skipped/newer
// non-target commit (Pick), all in range order. Unlike BuildSquash it does NOT
// require adjacency: the skipped in-between commits are reordered to just after
// the squashed commit, preserving their relative order. Conflicts surface
// through the normal rebase-conflict path.
//
// Errors when fewer than 2 targets are given or a target is not in the range.
func BuildSquashReorder(commits []model.RangeCommit, targets []string) (Plan, error) {
	idxs, isTarget, err := squashTargets(commits, targets)
	if err != nil {
		return Plan{}, err
	}
	entries := make([]Entry, 0, len(commits))
	// Targets first, oldest-first: oldest = Pick, the rest Squash into it.
	for n, i := range idxs {
		c := commits[i]
		action := Squash
		if n == 0 {
			action = Pick
		}
		entries = append(entries, Entry{Sha: c.Hash, Action: action, Orig: c.Message})
	}
	// Then every non-target commit, in range order, all Pick.
	for _, c := range commits {
		if !isTarget[c.Hash] {
			entries = append(entries, Entry{Sha: c.Hash, Action: Pick, Orig: c.Message})
		}
	}
	return Plan{Entries: entries}, nil
}

// shortSquashSHA trims a SHA for error messages without importing a TUI helper.
func shortSquashSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
