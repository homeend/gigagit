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
func (p Plan) RewriteTodo(ggBin, planPath string) (string, error) {
	groups, err := p.Groups()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, g := range groups {
		fmt.Fprintf(&b, "pick %s\n", p.Entries[g.Target].Sha)
		for _, si := range g.Squash {
			fmt.Fprintf(&b, "fixup %s\n", p.Entries[si].Sha)
		}
		if p.Entries[g.Target].Action == Reword || len(g.Squash) > 0 {
			fmt.Fprintf(&b, "exec %q __rebase-message %q %d\n", ggBin, planPath, g.Target)
		}
	}
	return b.String(), nil
}
