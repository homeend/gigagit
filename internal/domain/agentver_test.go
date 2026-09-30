package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/exttool"
)

// These tests swap the package-level probe seam, so they never run in
// parallel with each other (no t.Parallel).

func TestAgentVersionParsesAndCaches(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fakeagent")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	calls := 0
	old := agentVersionRun
	agentVersionRun = func(ctx context.Context, b string, args []string) ([]byte, error) {
		calls++
		return []byte("fakeagent 2.3.1\n"), nil
	}
	t.Cleanup(func() { agentVersionRun = old; resetAgentVersionCache() })
	tl := exttool.Tool{ID: "fake", VersionArgs: []string{"--version"}}
	for i := 0; i < 2; i++ {
		v, ok := AgentVersion(context.Background(), tl, bin)
		if !ok || v != (exttool.Version{2, 3, 1}) {
			t.Fatalf("got %v %v", v, ok)
		}
	}
	if calls != 1 {
		t.Fatalf("probe ran %d times, want 1 (cached)", calls)
	}
}

func TestAgentVersionUnknownWithoutArgsOrOnError(t *testing.T) {
	old := agentVersionRun
	agentVersionRun = func(context.Context, string, []string) ([]byte, error) { return nil, context.DeadlineExceeded }
	t.Cleanup(func() { agentVersionRun = old; resetAgentVersionCache() })
	if _, ok := AgentVersion(context.Background(), exttool.Tool{ID: "m"}, "meld"); ok {
		t.Fatal("no VersionArgs must be unknown")
	}
	if _, ok := AgentVersion(context.Background(), exttool.Tool{ID: "x", VersionArgs: []string{"--version"}}, "x-not-installed"); ok {
		t.Fatal("a failing probe must be unknown")
	}
}
