package rebaseplan

import (
	"fmt"

	"github.com/gigagit/gg/internal/model"
)

// BuildSquash builds the rebase plan that squashes the target commits into one,
// over an oldest-first commit range (git todo order). The oldest target (the
// smallest range index) stays Pick; every other target becomes Squash; all
// other commits keep Pick, with Orig carried from the range message. The targets
// must be adjacent in the range (no unselected commit between the oldest and
// newest target) — Stage 1 refuses gaps; reordering is a later stage.
//
// Errors when fewer than 2 targets are given, a target is not in the range, or
// the targets are not adjacent.
func BuildSquash(commits []model.RangeCommit, targets []string) (Plan, error) {
	if len(targets) < 2 {
		return Plan{}, fmt.Errorf("select at least 2 commits to squash")
	}
	pos := make(map[string]int, len(commits))
	for i, c := range commits {
		pos[c.Hash] = i
	}
	lo, hi := len(commits), -1
	isTarget := make(map[string]bool, len(targets))
	for _, t := range targets {
		i, ok := pos[t]
		if !ok {
			return Plan{}, fmt.Errorf("commit %s is not on the current branch", shortSquashSHA(t))
		}
		isTarget[t] = true
		if i < lo {
			lo = i
		}
		if i > hi {
			hi = i
		}
	}
	if hi-lo+1 != len(targets) {
		return Plan{}, fmt.Errorf("selected commits are not adjacent")
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

// shortSquashSHA trims a SHA for error messages without importing a TUI helper.
func shortSquashSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
