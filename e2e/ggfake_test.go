package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// The fake agent prints the canned review document scenarios review with.
func TestGGFakePrintsTheReview(t *testing.T) {
	t.Parallel()
	out, err := exec.Command(ggFakeBin, "review").Output()
	if err != nil || !strings.Contains(string(out), "## Overview") {
		t.Fatalf("ggfake review: %v\n%s", err, out)
	}
}

// {{ggfake}} expands to the bare name of the fake agent on PATH.
func TestExpandTextGGFake(t *testing.T) {
	t.Parallel()
	if got := ExpandText("{{ggfake}} review"); got != "ggfake review" {
		t.Fatalf("ExpandText = %q", got)
	}
	if p, err := exec.LookPath("ggfake"); err != nil || p != ggFakeBin {
		t.Fatalf("ggfake on PATH = %q, %v; want %q", p, err, ggFakeBin)
	}
}
