package rebaseplan

import (
	"fmt"
	"strings"
)

// Group is a target commit plus the squash entries melded into it, by index
// into Plan.Entries.
type Group struct {
	Target int
	Squash []int
}

// Groups returns the squash-groups in todo order, skipping Drop entries. Each
// Squash entry attaches to the nearest preceding non-squash target. It errors
// if a squash has no preceding target (the editor forbids squash on the oldest
// row, so this only happens on a malformed plan).
func (p Plan) Groups() ([]Group, error) {
	var groups []Group
	cur := -1 // index into groups of the current target's group
	for i, e := range p.Entries {
		switch e.Action {
		case Drop:
			continue
		case Squash:
			if cur < 0 {
				return nil, fmt.Errorf("rebaseplan: squash at index %d has no preceding commit", i)
			}
			groups[cur].Squash = append(groups[cur].Squash, i)
		default: // Pick, Reword
			groups = append(groups, Group{Target: i})
			cur = len(groups) - 1
		}
	}
	return groups, nil
}

// Message returns the commit message for the group whose target is at index ti:
// the target's new message (if reworded) else its original, kept verbatim; then,
// when there are squashed commits, a blank line (git's subject/body separator)
// followed by each squashed commit's message stacked line-by-line in the body.
func (p Plan) Message(ti int) string {
	t := p.Entries[ti]
	base := t.Orig
	if t.Action == Reword && t.NewMsg != "" {
		base = t.NewMsg
	}
	msg := strings.TrimRight(base, "\n")
	var squashed []string
	for i := ti + 1; i < len(p.Entries); i++ {
		switch p.Entries[i].Action {
		case Squash:
			squashed = append(squashed, strings.TrimRight(p.Entries[i].Orig, "\n"))
		case Drop:
			continue
		default:
			i = len(p.Entries) // next target ends the group
		}
	}
	if len(squashed) > 0 {
		msg += "\n\n" + strings.Join(squashed, "\n")
	}
	return msg
}
