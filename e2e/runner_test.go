package e2e

import (
	"bytes"
	"testing"
)

func TestCLIRunnerRunsStatus(t *testing.T) {
	sb := buildSandbox(t, localScenario([]Step{
		{Write: "a.txt", Content: "v1\n"},
		{Commit: "initial"},
	}))
	var out bytes.Buffer
	if code := (CLIRunner{}).Run(sb.LocalDir, []string{"status"}, &out, &out); code != 0 {
		t.Fatalf("status exit = %d, want 0\n%s", code, out.String())
	}
	out.Reset()
	// unknown subcommand → usage error (exit 2)
	if code := (CLIRunner{}).Run(sb.LocalDir, []string{"nonsense"}, &out, &out); code != 2 {
		t.Fatalf("nonsense exit = %d, want 2", code)
	}
}
