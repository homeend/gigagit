// Package rebaseplan models an interactive-rebase plan and turns it into the
// git rebase todo + per-commit messages. It is pure: no git, no os/exec — the
// gg-as-editor subcommands in cmd/gg do the I/O and run git.
package rebaseplan

import "encoding/json"

// Action is one interactive-rebase row action.
type Action string

const (
	Pick   Action = "pick"
	Reword Action = "reword"
	Squash Action = "squash"
	Drop   Action = "drop"
)

// Entry is one commit in the plan. Orig is the commit's original full message
// (always populated by the builder); NewMsg holds the user's new message for a
// Reword. Entries are stored in git todo order (oldest-first).
type Entry struct {
	Sha    string `json:"sha"`
	Action Action `json:"action"`
	Orig   string `json:"orig"`
	NewMsg string `json:"new_msg,omitempty"`
}

// Plan is the ordered set of rebase entries.
type Plan struct {
	Entries []Entry `json:"entries"`
}

// Marshal serializes the plan to JSON for the plan file.
func Marshal(p Plan) ([]byte, error) { return json.Marshal(p) }

// Unmarshal parses a plan file's JSON.
func Unmarshal(b []byte) (Plan, error) {
	var p Plan
	err := json.Unmarshal(b, &p)
	return p, err
}
