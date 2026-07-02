package gitconfdocs

import (
	"os/exec"
	"strings"
	"testing"
)

func TestLookupIsCaseInsensitive(t *testing.T) {
	if Lookup("fetch.writeCommitGraph") == nil {
		t.Fatal("curated key missing by exact case")
	}
	if Lookup("fetch.writecommitgraph") == nil {
		t.Fatal("lookup must be case-insensitive (git lowercases set keys)")
	}
	if Lookup("no.such.key") != nil {
		t.Fatal("non-curated key must return nil")
	}
}

func TestTableShape(t *testing.T) {
	docs := All()
	if len(docs) < 55 {
		t.Fatalf("curated table has %d entries, want ~60", len(docs))
	}
	seen := map[string]bool{}
	for _, d := range docs {
		lk := strings.ToLower(d.Key)
		if seen[lk] {
			t.Fatalf("duplicate curated key %q", d.Key)
		}
		seen[lk] = true
		if d.Desc == "" {
			t.Fatalf("%s: description required", d.Key)
		}
		if d.Kind == KindEnum && len(d.Options) < 2 {
			t.Fatalf("%s: enum kind needs options", d.Key)
		}
		if d.Kind != KindEnum && len(d.Options) != 0 {
			t.Fatalf("%s: options only belong on enums", d.Key)
		}
	}
}

// TestCuratedKeysExistInGitCatalog is the staleness gate: every curated key
// must still be a real key in THIS machine's `git help -c` output, so a git
// rename/removal breaks the build here instead of shipping stale docs.
func TestCuratedKeysExistInGitCatalog(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	out, err := exec.Command("git", "help", "-c").Output()
	if err != nil {
		t.Skipf("git help -c unavailable: %v", err)
	}
	catalog := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			catalog[strings.ToLower(line)] = true
		}
	}
	for _, d := range All() {
		if !catalog[strings.ToLower(d.Key)] {
			t.Errorf("curated key %q not in git help -c — stale table entry", d.Key)
		}
	}
}
