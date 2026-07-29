package rebaseplan

import (
	"fmt"
	"strings"
)

// RewriteTodo produces the git interactive-rebase todo text for the plan.
// ggBin is the gg binary path and planPath the plan file path; together they
// form the `exec` lines that apply reword/squash messages. A reworded target or
// a target with squashed commits gets one exec line (referencing the target's
// original index) after its pick (+ fixups).
//
// A Drop entry renders as an explicit `drop` line, never by omission: when the
// whole range is dropped (e.g. dropping the branch tip, whose range holds only
// that commit) an empty todo would make git abort with "error: nothing to do".
//
// goos selects the path quoting: git runs an exec line through its own POSIX
// sh on every platform, so a Windows path needs forward slashes (see
// ShellPath).
func (p Plan) RewriteTodo(ggBin, planPath, goos string) (string, error) {
	groups, err := p.Groups()
	if err != nil {
		return "", err
	}
	byTarget := make(map[int]Group, len(groups))
	for _, g := range groups {
		byTarget[g.Target] = g
	}
	var b strings.Builder
	for i, e := range p.Entries {
		switch e.Action {
		case Drop:
			fmt.Fprintf(&b, "drop %s\n", e.Sha)
		case Squash:
			// rendered as a fixup under its group's target
		default: // Pick, Reword
			g := byTarget[i]
			fmt.Fprintf(&b, "pick %s\n", e.Sha)
			for _, si := range g.Squash {
				fmt.Fprintf(&b, "fixup %s\n", p.Entries[si].Sha)
			}
			if e.Action == Reword || len(g.Squash) > 0 {
				fmt.Fprintf(&b, "exec %s __rebase-message %s %d\n", ShellPath(ggBin, goos), ShellPath(planPath, goos), i)
			}
		}
	}
	return b.String(), nil
}
