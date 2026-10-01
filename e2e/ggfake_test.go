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

// {{ggfake}} expands to the fake agent's path, quoted for the shell capture
// commands run through.
func TestExpandTextGGFake(t *testing.T) {
	t.Parallel()
	if got := ExpandText("{{ggfake}} review"); got != shellQuote(ggFakeBin)+" review" || ggFakeBin == "" {
		t.Fatalf("ExpandText = %q (ggFakeBin %q)", got, ggFakeBin)
	}
}
