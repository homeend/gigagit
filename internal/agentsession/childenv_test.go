package agentsession

import (
	"slices"
	"testing"
)

func TestChildEnvDropsTheHostsAgentChannel(t *testing.T) {
	got := childEnv([]string{"PATH=/bin", "GG_SESSION_ID=p/s1", "GG_MCP_URL=http://x", "GG_SESSION_TOKEN=abc", "GG_PARENT_SESSION=p/s0", "TMUX=1", "GG_INBOX=/i"})
	if !slices.Equal(got, []string{"PATH=/bin", "GG_INBOX=/i"}) {
		t.Fatalf("childEnv = %v", got)
	}
}
