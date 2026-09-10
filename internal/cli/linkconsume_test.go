package cli

import (
	"bytes"
	"os"
	"os/exec"
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

// TestShowRejectsALinkCombinedWithPaths covers the show.go:35-38 guard: a
// COMMIT link (not a working-tree one, so the "needs a commit" refusal can't
// also explain the exit 2) combined with -- <paths> is a usage error, not a
// silent narrowing.
func TestShowRejectsALinkCombinedWithPaths(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	var out bytes.Buffer
	if code := runLink(linkState(t), openCLI(dir), dir, []string{"--rev", head}, &out, os.Stderr); code != 0 {
		t.Fatal("gg link --rev failed")
	}
	link := strings.TrimSpace(out.String())
	code, _, errb := runCLI(t, dir, "show", link, "--", "README.md")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr %q", code, errb)
	}
	if !strings.Contains(errb, "-- <paths>") {
		t.Errorf("stderr = %q, want it to name the -- <paths> rule", errb)
	}
}

// linkTwoCommitFixture is a repo with two commits, the second changing
// README.md and adding other.txt — a NON-root commit (so the "<sha>^..<sha>"
// range flag form resolves) that touches more than one file (so a
// path-scoped link is a strictly narrower view than a repo-scoped one).
func linkTwoCommitFixture(t *testing.T) (dir, sha1, sha2 string) {
	t.Helper()
	dir = newCLIRepo(t)
	sha1 = strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nthere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, args := range [][]string{{"add", "README.md", "other.txt"}, {"commit", "-m", "second"}} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = env
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	sha2 = strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	return dir, sha1, sha2
}

// TestDiffWithAStagedLinkMatchesTheFlags is spec §7 parity for the STAGED
// state: `gg diff <staged link>` must match `gg diff --cached -- <path>`
// byte for byte. (The "--" is load-bearing on the flag side: without it,
// `gg diff --cached README.md` reads README.md as a rev and fails.)
func TestDiffWithAStagedLinkMatchesTheFlags(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nthere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, dir, "add", "README.md")
	var out bytes.Buffer
	if code := runLink(linkState(t), openCLI(dir), dir, []string{"--cached", "README.md"}, &out, os.Stderr); code != 0 {
		t.Fatal("gg link --cached failed")
	}
	link := strings.TrimSpace(out.String())
	_, viaLink, errb := runCLI(t, dir, "diff", link)
	if viaLink == "" {
		t.Fatalf("diff %s produced nothing (stderr %q)", link, errb)
	}
	_, viaFlags, errb2 := runCLI(t, dir, "diff", "--cached", "--", "README.md")
	if viaFlags == "" {
		t.Fatalf("diff --cached -- README.md produced nothing (stderr %q)", errb2)
	}
	if viaLink != viaFlags {
		t.Errorf("staged link diff differs from the flag diff:\n%q\nvs\n%q", viaLink, viaFlags)
	}
}

// TestDiffWithACommitLinkMatchesTheRangeFlagForm is spec §7 parity for the
// COMMIT state, done properly: gg diff has no --rev flag, and the default
// (non --hunks) patch path never calls HunkDiffSpec — only --hunks does
// (diff.go). So there is no bare "gg diff <sha>" flag form producing the
// commit's OWN change; the true CLI equivalent is the range HunkDiffSpec
// itself builds for a non-root commit: "<sha>^..<sha>". Passing that range
// positional hits the SAME DiffSpec{Rev: "<sha>^..<sha>"} the link path
// builds, so the two renders are identical by construction.
func TestDiffWithACommitLinkMatchesTheRangeFlagForm(t *testing.T) {
	t.Parallel()
	dir, _, sha2 := linkTwoCommitFixture(t)
	var out bytes.Buffer
	if code := runLink(linkState(t), openCLI(dir), dir, []string{"--rev", sha2, "README.md"}, &out, os.Stderr); code != 0 {
		t.Fatal("gg link --rev failed")
	}
	link := strings.TrimSpace(out.String())
	_, viaLink, errb := runCLI(t, dir, "diff", link)
	if viaLink == "" {
		t.Fatalf("diff %s produced nothing (stderr %q)", link, errb)
	}
	_, viaFlags, errb2 := runCLI(t, dir, "diff", sha2+"^.."+sha2, "--", "README.md")
	if viaFlags == "" {
		t.Fatalf("diff %s^..%s -- README.md produced nothing (stderr %q)", sha2, sha2, errb2)
	}
	if viaLink != viaFlags {
		t.Errorf("commit link diff differs from the range-flag diff:\n%q\nvs\n%q", viaLink, viaFlags)
	}
}

// TestDiffHunksWithALinkMatchesTheFlags is spec §7 parity for --hunks: a
// commit link's hunk numbering must match `gg diff --hunks <sha> -- <path>`
// (the positional-rev flag form, the one that actually routes through
// HunkDiffSpec) — this is what makes a hunk number gg diff --hunks <link>
// prints the same one gg note add <link>#N anchors.
func TestDiffHunksWithALinkMatchesTheFlags(t *testing.T) {
	t.Parallel()
	dir, _, sha2 := linkTwoCommitFixture(t)
	var out bytes.Buffer
	if code := runLink(linkState(t), openCLI(dir), dir, []string{"--rev", sha2, "README.md"}, &out, os.Stderr); code != 0 {
		t.Fatal("gg link --rev failed")
	}
	link := strings.TrimSpace(out.String())
	_, viaLink, errb := runCLI(t, dir, "diff", "--hunks", link)
	if viaLink == "" {
		t.Fatalf("diff --hunks %s produced nothing (stderr %q)", link, errb)
	}
	_, viaFlags, errb2 := runCLI(t, dir, "diff", "--hunks", sha2, "--", "README.md")
	if viaFlags == "" {
		t.Fatalf("diff --hunks %s -- README.md produced nothing (stderr %q)", sha2, errb2)
	}
	if viaLink != viaFlags {
		t.Errorf("commit link hunks differ from the flag hunks:\n%q\nvs\n%q", viaLink, viaFlags)
	}
	if !strings.Contains(viaLink, "  1 @@") {
		t.Errorf("viaLink = %q, want hunk 1 listed", viaLink)
	}
}

// TestShowWithAPathLinkIsNarrowerThanTheCommitLink: a commit that touches
// two files, shown through a path-scoped link, must exclude the other file
// — gg show <link> limits --patch to the link's path when one is present.
func TestShowWithAPathLinkIsNarrowerThanTheCommitLink(t *testing.T) {
	t.Parallel()
	dir, _, sha2 := linkTwoCommitFixture(t)
	var outPath, outRepo bytes.Buffer
	if code := runLink(linkState(t), openCLI(dir), dir, []string{"--rev", sha2, "README.md"}, &outPath, os.Stderr); code != 0 {
		t.Fatal("gg link --rev README.md failed")
	}
	if code := runLink(linkState(t), openCLI(dir), dir, []string{"--rev", sha2}, &outRepo, os.Stderr); code != 0 {
		t.Fatal("gg link --rev failed")
	}
	pathLink := strings.TrimSpace(outPath.String())
	repoLink := strings.TrimSpace(outRepo.String())
	_, pathOut, errb1 := runCLI(t, dir, "show", pathLink, "--patch")
	if pathOut == "" {
		t.Fatalf("show %s --patch produced nothing (stderr %q)", pathLink, errb1)
	}
	_, repoOut, errb2 := runCLI(t, dir, "show", repoLink, "--patch")
	if repoOut == "" {
		t.Fatalf("show %s --patch produced nothing (stderr %q)", repoLink, errb2)
	}
	if strings.Contains(pathOut, "other.txt") {
		t.Errorf("path-scoped show = %q, want it to exclude other.txt", pathOut)
	}
	if !strings.Contains(repoOut, "other.txt") {
		t.Errorf("repo-scoped show = %q, want it to include other.txt", repoOut)
	}
}
