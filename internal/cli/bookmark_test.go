package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bookmarkRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return newRepoDir(t)
}

func TestBookmarkLivePointerRoundTrip(t *testing.T) {
	dir := bookmarkRepo(t)
	// README.md committed as "hi\n"; make a working edit.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errb := runCLI(t, dir, "bookmark", "add", "README.md")
	if code != 0 {
		t.Fatalf("add exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(out)
	if id == "" {
		t.Fatalf("add printed no id")
	}

	// Edit AGAIN after bookmarking — a live pointer must reflect the new bytes.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errb = runCLI(t, dir, "bookmark", "paste", id, "out.txt")
	if code != 0 {
		t.Fatalf("paste exit %d: %s", code, errb)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "out.txt")); string(got) != "v2\n" {
		t.Fatalf("pasted = %q, want v2 (live)", got)
	}
}

func TestBookmarkPasteRequiresDest(t *testing.T) {
	dir := bookmarkRepo(t)
	if code, _, _ := runCLI(t, dir, "bookmark", "paste", "some-id"); code != 2 {
		t.Fatalf("missing dest should exit 2, got %d", code)
	}
}

func TestBookmarkUsageErrors(t *testing.T) {
	dir := bookmarkRepo(t)
	if code, _, _ := runCLI(t, dir, "bookmark"); code != 2 {
		t.Fatalf("bare bookmark should exit 2, got %d", code)
	}
	if code, _, _ := runCLI(t, dir, "bookmark", "add"); code != 2 {
		t.Fatalf("add without paths should exit 2, got %d", code)
	}
}
