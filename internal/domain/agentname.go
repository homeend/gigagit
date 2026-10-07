package domain

import (
	"strings"
	"unicode"

	"github.com/homeend/gigagit/internal/agentsession"
)

// AgentNameHistoryScope is the per-repo search-history ring that remembers
// the names typed in Start agent (TUI alt+↓ recall, the web's datalist).
const AgentNameHistoryScope = "agentname"

// maxAgentNameRunes caps a session name: a row label, not a description.
const maxAgentNameRunes = 40

// CleanAgentName is the one normalisation of a user-typed session name:
// every run of control characters (newlines and tabs included) and
// whitespace becomes one space — a control separates words, never glues
// them — then the result is trimmed and cut to 40 runes (no trailing
// space). Whitespace only is no name.
func CleanAgentName(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}), " ")
	if r := []rune(s); len(r) > maxAgentNameRunes {
		s = strings.TrimRight(string(r[:maxAgentNameRunes]), " ")
	}
	return s
}

// SessionTitle is how a session is shown: "Claude (yolo) [viewer]", or the
// bare label when unnamed (agentsession.Title, for frontends).
func SessionTitle(label, name string) string { return agentsession.Title(label, name) }
