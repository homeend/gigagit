package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseBlamePorcelain(t *testing.T) {
	t.Parallel()
	// Full header on a commit's first appearance; abbreviated (sha + line nums)
	// on repeats; a renamed commit carries previous/filename (ignored cleanly);
	// the all-zero sha is an uncommitted working-tree line.
	data := "" +
		"1111111111111111111111111111111111111111 1 1 2\n" +
		"author Ada\n" +
		"author-mail <a@x>\n" +
		"author-time 1700000000\n" +
		"author-tz +0000\n" +
		"committer Ada\n" +
		"committer-time 1700000000\n" +
		"committer-tz +0000\n" +
		"summary first commit\n" +
		"filename a.go\n" +
		"\tpackage main\n" +
		"1111111111111111111111111111111111111111 2 2\n" +
		"\tfunc main() {}\n" +
		"2222222222222222222222222222222222222222 3 3 1\n" +
		"author Bob\n" +
		"author-time 1690000000\n" +
		"author-tz +0000\n" +
		"summary second\n" +
		"previous 9999999999999999999999999999999999999999 old.go\n" +
		"filename a.go\n" +
		"\tnew line\n" +
		"0000000000000000000000000000000000000000 4 4 1\n" +
		"author Not Committed Yet\n" +
		"author-time 1680000000\n" +
		"author-tz +0000\n" +
		"summary Version of a.go from a.go\n" +
		"filename a.go\n" +
		"\tdirty line\n"

	got := ParseBlamePorcelain([]byte(data))
	if len(got) != 4 {
		t.Fatalf("want 4 blame lines, got %d: %+v", len(got), got)
	}
	if got[0].Hash != "1111111111111111111111111111111111111111" ||
		got[0].Author != "Ada" || got[0].Time != 1700000000 ||
		got[0].Summary != "first commit" || got[0].LineNo != 1 ||
		got[0].Content != "package main" {
		t.Errorf("line 0 wrong: %+v", got[0])
	}
	// Repeat of sha1: metadata reused from the first appearance.
	if got[1].Hash != "1111111111111111111111111111111111111111" ||
		got[1].Author != "Ada" || got[1].LineNo != 2 || got[1].Content != "func main() {}" {
		t.Errorf("line 1 (abbreviated repeat) wrong: %+v", got[1])
	}
	if got[2].Hash != "2222222222222222222222222222222222222222" ||
		got[2].Author != "Bob" || got[2].Content != "new line" || got[2].LineNo != 3 {
		t.Errorf("line 2 (renamed commit) wrong: %+v", got[2])
	}
	// Uncommitted: zero sha normalised to "".
	if got[3].Hash != "" || got[3].Author != "Not Committed Yet" ||
		got[3].Content != "dirty line" || got[3].LineNo != 4 {
		t.Errorf("line 3 (uncommitted) wrong: %+v", got[3])
	}
}

func TestBlameVerb(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // repo with an initial commit (README.md)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a.go")
	gitIn(t, dir, "commit", "-m", "first")

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("line one\nline two edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a.go")
	gitIn(t, dir, "commit", "-m", "second")

	got, err := repo.Blame(context.Background(), "", "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 blame lines, got %d: %+v", len(got), got)
	}
	if got[0].Content != "line one" || got[1].Content != "line two edited" {
		t.Errorf("content wrong: %+v", got)
	}
	if got[0].Hash == "" || got[1].Hash == "" {
		t.Errorf("committed lines should have shas: %+v", got)
	}
	if got[0].Hash == got[1].Hash {
		t.Errorf("the two lines come from different commits, want distinct shas: %+v", got)
	}
}
