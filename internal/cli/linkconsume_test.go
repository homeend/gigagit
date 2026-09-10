package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linkFixture is a repo with a committed file and a working-tree edit, plus
// the link that names that file in the working tree.
func linkFixture(t *testing.T) (dir, link string) {
	t.Helper()
	dir = newCLIRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nthere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runLink(linkState(t), openCLI(dir), dir, []string{"README.md"}, &out, os.Stderr); code != 0 {
		t.Fatalf("gg link: exit %d", code)
	}
	return dir, strings.TrimSpace(out.String())
}

func TestDiffWithALinkMatchesTheEquivalentFlags(t *testing.T) {
	t.Parallel()
	dir, link := linkFixture(t)
	_, viaLink, errb := runCLI(t, dir, "diff", link)
	if viaLink == "" {
		t.Fatalf("diff %s produced nothing (stderr %q)", link, errb)
	}
	_, viaFlags, _ := runCLI(t, dir, "diff", "--", "README.md")
	if viaLink != viaFlags {
		t.Errorf("link diff differs from the flag diff:\n%q\nvs\n%q", viaLink, viaFlags)
	}
}

func TestDiffWithACommitLinkShowsThatCommitsOwnChange(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	var out bytes.Buffer
	if code := runLink(linkState(t), openCLI(dir), dir, []string{"--rev", "HEAD", "README.md"}, &out, os.Stderr); code != 0 {
		t.Fatal("gg link --rev failed")
	}
	link := strings.TrimSpace(out.String())
	code, viaLink, errb := runCLI(t, dir, "diff", link)
	if code != 0 {
		t.Fatalf("exit %d (stderr %q)", code, errb)
	}
	// The commit's own change: HunkDiffSpec's parent→commit rule, which for a
	// root commit is the empty tree — so the seeded file shows as added.
	if !strings.Contains(viaLink, "README.md") || !strings.Contains(viaLink, "+++") {
		t.Errorf("diff %s = %q, want the commit's own patch", head, viaLink)
	}
}

func TestDiffRejectsALinkCombinedWithATargetFlag(t *testing.T) {
	t.Parallel()
	dir, link := linkFixture(t)
	for _, args := range [][]string{
		{"diff", "--cached", link},
		{"diff", link, "--", "README.md"},
	} {
		if code, _, errb := runCLI(t, dir, args...); code != 2 {
			t.Errorf("%v: exit = %d, want 2; stderr %q", args, code, errb)
		}
	}
}

// The old parsing is untouched: a positional that is not a gg:// link is
// still a rev.
func TestDiffStillTakesAPlainRev(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	if code, _, errb := runCLI(t, dir, "diff", "HEAD"); code != 0 {
		t.Errorf("exit = %d, want 0; stderr %q", code, errb)
	}
}

func TestShowWithACommitLink(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	var out bytes.Buffer
	if code := runLink(linkState(t), openCLI(dir), dir, []string{"--rev", "HEAD"}, &out, os.Stderr); code != 0 {
		t.Fatal("gg link --rev failed")
	}
	link := strings.TrimSpace(out.String())
	code, stdout, errb := runCLI(t, dir, "show", link)
	if code != 0 {
		t.Fatalf("exit %d (stderr %q)", code, errb)
	}
	// gg show prints the ABBREVIATED sha (git log --format=%h, the same as
	// every other gg show/log line) — so check it the other way round: the
	// full sha this test resolved starts with what gg show printed.
	fields := strings.Fields(stdout)
	if len(fields) == 0 || !strings.HasPrefix(head, fields[0]) {
		t.Errorf("stdout = %q, want it to start with (an abbreviation of) %s", stdout, head)
	}
}

func TestShowRejectsAWorkingTreeLink(t *testing.T) {
	t.Parallel()
	dir, link := linkFixture(t)
	code, _, errb := runCLI(t, dir, "show", link)
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr %q", code, errb)
	}
	if !strings.Contains(errb, "commit") {
		t.Errorf("stderr = %q, want it to say a commit is needed", errb)
	}
}
