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
// trimmed, control characters (newlines and tabs included) dropped, cut to
// 40 runes. Whitespace only is no name.
func CleanAgentName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > maxAgentNameRunes {
		s = string(r[:maxAgentNameRunes])
	}
	return s
}

// SessionTitle is how a session is shown: "Claude (yolo) [viewer]", or the
// bare label when unnamed (agentsession.Title, for frontends).
func SessionTitle(label, name string) string { return agentsession.Title(label, name) }
