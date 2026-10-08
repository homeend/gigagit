package cli

import (
	"strings"
	"testing"
)

// Spec §1: `gg pr notes` lists what the PR's view holds — a note written for
// the PR, never a plain note on its head.
func TestPRNotesListsOnlyThePRsNotes(t *testing.T) {
	dir, head, _ := sendPRRepo(t)
	mine := addCLINote(t, dir, head, 5) // --preview main...refs/gg/pr/7
	var out, errb strings.Builder
	if code := Run(dir, []string{"note", "add", "--rev", head, "--file", "big.go", "--new-line", "25",
		"--summary", "a commit note", "--source", "user"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("note add: %s", errb.String())
	}
	out.Reset()
	if code := Run(dir, []string{"pr", "notes", "7"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("pr notes: %s", errb.String())
	}
	if !strings.Contains(out.String(), "look here") || strings.Contains(out.String(), "a commit note") {
		t.Fatalf("pr notes (want %s):\n%s", mine, out.String())
	}
}
