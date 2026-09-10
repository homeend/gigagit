package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/repos"
)

// twoNoteRepos builds A (the cwd) and B (another checkout holding a note) and
// returns both paths, B's repository link and the id of B's note. B is
// registered in a scratch repo registry, as any checkout gg has opened once
// is — that is how a link from another cwd finds it.
func twoNoteRepos(t *testing.T) (repoA, repoB, linkB, id string) {
	t.Helper()
	repoA = noteRepo(t) // sets XDG_STATE_HOME once; B shares it
	repoB = t.TempDir()
	statePath := filepath.Join(t.TempDir(), "repos.toml")
	prevState := RepoStatePath
	RepoStatePath = statePath
	t.Cleanup(func() { RepoStatePath = prevState })
	if err := repos.Touch(statePath, repoB, "", time.Now()); err != nil {
		t.Fatalf("repos.Touch: %v", err)
	}
	runGit(t, repoB, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoB, "b.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoB, "add", "b.txt")
	runGit(t, repoB, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(repoB, "b.txt"), []byte("one\nTWO\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLI(t, repoB, "note", "add", "--file", "b.txt", "--new-line", "2", "--summary", "in B")
	if code != 0 {
		t.Fatalf("seed note in B: exit %d (%s)", code, errb)
	}
	id = strings.TrimSpace(out)
	code, out, errb = runCLI(t, repoB, "link")
	if code != 0 {
		t.Fatalf("gg link in B: exit %d (%s)", code, errb)
	}
	return repoA, repoB, strings.TrimSpace(out), id
}

// TestNoteReplyUnknownIDNamesTheStoreItSearched: the user's own trip-up —
// `gg note reply <id>` run from the WRONG checkout. Note ids are per
// repository, so the message must say which store was searched and how to
// reach the right one, instead of the old "error: note: notes: not found".
func TestNoteReplyUnknownIDNamesTheStoreItSearched(t *testing.T) {
	repoA, _, _, id := twoNoteRepos(t)
	code, _, errb := runCLI(t, repoA, "note", "reply", id, "--summary", "1 + 1 = 2")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr %q", code, errb)
	}
	for _, want := range []string{"note reply: no note " + id, "gg://", "checkout"} {
		if !strings.Contains(errb, want) {
			t.Errorf("stderr = %q, want it to contain %q", errb, want)
		}
	}
	if strings.Contains(errb, "notes: not found") || strings.Contains(errb, "error:") {
		t.Errorf("stderr = %q still carries the old doubled prefix", errb)
	}
	if code, _, errb := runCLI(t, repoA, "note", "rm", id); code != 1 || !strings.Contains(errb, "note rm: no note "+id) {
		t.Errorf("rm: exit %d stderr %q, want the same shape", code, errb)
	}
}

// TestNoteReplyAndRmTakeARepositoryLink: the fix for that trip-up. A
// repository link as the first positional picks the checkout whose store holds
// the id; the reply lands in B and A's store stays empty.
func TestNoteReplyAndRmTakeARepositoryLink(t *testing.T) {
	repoA, repoB, linkB, id := twoNoteRepos(t)
	code, out, errb := runCLI(t, repoA, "note", "reply", linkB, id, "--summary", "1 + 1 = 2")
	if code != 0 {
		t.Fatalf("reply via link: exit %d (%s)", code, errb)
	}
	replyID := strings.TrimSpace(out)
	if replyID == "" || replyID == id {
		t.Fatalf("reply id = %q", replyID)
	}
	if _, listB, _ := runCLI(t, repoB, "note", "list", "--file", "b.txt", "--json"); !strings.Contains(listB, "1 + 1 = 2") {
		t.Errorf("B's notes = %s, want the reply", listB)
	}
	if _, listA, _ := runCLI(t, repoA, "note", "list", "--json"); strings.Contains(listA, "1 + 1 = 2") {
		t.Errorf("A's notes = %s: the reply leaked into the cwd's store", listA)
	}
	if code, _, errb := runCLI(t, repoA, "note", "rm", linkB, id); code != 0 {
		t.Fatalf("rm via link: exit %d (%s)", code, errb)
	}
	if _, listB, _ := runCLI(t, repoB, "note", "list", "--json"); strings.Contains(listB, "in B") {
		t.Errorf("B still holds the root after rm: %s", listB)
	}
}

// TestNoteReplyAndRmRefuseAFileLink: the id names the note, so a link
// carrying a file or a target has parts the verb cannot use — a usage error,
// never a silent override.
func TestNoteReplyAndRmRefuseAFileLink(t *testing.T) {
	repoA, _, linkB, id := twoNoteRepos(t)
	fileLink := linkB + "/b.txt:2"
	for _, args := range [][]string{
		{"note", "reply", fileLink, id, "--summary", "s"},
		{"note", "rm", fileLink, id},
		{"note", "reply", linkB + "@staged", id, "--summary", "s"},
	} {
		code, _, errb := runCLI(t, repoA, args...)
		if code != 2 || !strings.Contains(errb, "repository") {
			t.Errorf("%v: exit %d stderr %q, want 2 + a 'pass the repository link' message", args, code, errb)
		}
	}
}

// TestNoteClearTakesALink: a repository link selects the checkout (then
// --file/--all as usual); a file link replaces --file/--cached/--rev exactly
// as `note list <link>` does.
func TestNoteClearTakesALink(t *testing.T) {
	repoA, repoB, linkB, _ := twoNoteRepos(t)
	if code, _, errb := runCLI(t, repoA, "note", "clear", linkB+"/b.txt", "--file", "b.txt", "--yes"); code != 2 || !strings.Contains(errb, "drop --file") {
		t.Errorf("file link + --file: exit %d stderr %q, want 2", code, errb)
	}
	if code, _, errb := runCLI(t, repoA, "note", "clear", linkB+"/b.txt", "--all", "--yes"); code != 2 {
		t.Errorf("file link + --all: exit %d stderr %q, want 2", code, errb)
	}
	// A path-less link with a target has nothing to apply that target to:
	// refused, not silently treated as the bare repository link.
	if code, _, errb := runCLI(t, repoA, "note", "clear", linkB+"@staged", "--all", "--yes"); code != 2 || !strings.Contains(errb, "repository link cannot carry") {
		t.Errorf("repo link + @staged: exit %d stderr %q, want 2", code, errb)
	}
	code, out, errb := runCLI(t, repoA, "note", "clear", linkB+"/b.txt", "--yes")
	if code != 0 || !strings.Contains(out, "removed 1 notes") {
		t.Fatalf("clear via file link: exit %d out %q err %q", code, out, errb)
	}
	// Re-seed and clear through the repository form.
	if code, _, errb := runCLI(t, repoB, "note", "add", "--file", "b.txt", "--new-line", "2", "--summary", "again"); code != 0 {
		t.Fatalf("re-seed: %d %s", code, errb)
	}
	code, out, errb = runCLI(t, repoA, "note", "clear", linkB, "--all", "--yes")
	if code != 0 || !strings.Contains(out, "removed 1 notes") {
		t.Fatalf("clear via repo link: exit %d out %q err %q", code, out, errb)
	}
}

// TestNoteApplyTakesALink: a repository link selects the checkout; its target
// (`@staged` / `@<sha>`) replaces --cached / --rev; a path on the link is
// refused for the same reason --file is.
func TestNoteApplyTakesALink(t *testing.T) {
	repoA, repoB, linkB, _ := twoNoteRepos(t)
	batch := `{"comments":[{"filePath":"b.txt","newLine":2,"summary":"from A"}]}`
	code, out, errb := runCLIStdin(t, repoA, batch, "note", "apply", linkB, "--stdin")
	if code != 0 {
		t.Fatalf("apply via repo link: exit %d out %q err %q", code, out, errb)
	}
	if _, listB, _ := runCLI(t, repoB, "note", "list", "--file", "b.txt", "--json"); !strings.Contains(listB, "from A") {
		t.Errorf("B's notes = %s, want the applied note", listB)
	}
	if code, _, errb := runCLIStdin(t, repoA, batch, "note", "apply", linkB+"@staged", "--stdin", "--cached"); code != 2 || !strings.Contains(errb, "--cached") {
		t.Errorf("target link + --cached: exit %d stderr %q, want 2", code, errb)
	}
	if code, _, errb := runCLIStdin(t, repoA, batch, "note", "apply", linkB+"/b.txt", "--stdin"); code != 2 || !strings.Contains(errb, "path") {
		t.Errorf("file link: exit %d stderr %q, want 2", code, errb)
	}
}

// TestNoteLinkMustBeFirstArgumentEverywhere extends the add/list rule to the
// four verbs that newly take a link.
func TestNoteLinkMustBeFirstArgumentEverywhere(t *testing.T) {
	repoA, _, linkB, id := twoNoteRepos(t)
	for _, args := range [][]string{
		{"note", "reply", id, "--summary", "s", linkB},
		{"note", "rm", id, linkB},
		{"note", "clear", "--all", "--yes", linkB},
	} {
		code, _, errb := runCLI(t, repoA, args...)
		if code != 2 || !strings.Contains(errb, "first argument") {
			t.Errorf("%v: exit %d stderr %q, want 2 + 'first argument'", args, code, errb)
		}
	}
}
