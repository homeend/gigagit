package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentskill"
)

func TestSkillPathMaterialisesAndPrints(t *testing.T) {
	cache := t.TempDir()
	old := SkillCacheDir
	SkillCacheDir = cache
	t.Cleanup(func() { SkillCacheDir = old })

	dir := newRepoDir(t)
	code, out, errb := runCLI(t, dir, "skill", "path")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	p := strings.TrimSpace(out)
	if !filepath.IsAbs(p) {
		t.Fatalf("stdout = %q, want an absolute path", p)
	}
	want := filepath.Join(cache, "gg", "skills", "reviewing-with-gg", "SKILL.md")
	if p != want {
		t.Fatalf("path = %q, want %q (review is the default)", p, want)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != agentskill.ReviewingWithGG.SkillFile() {
		t.Fatal("the written file must be the embedded skill")
	}

	// Second call: same path, and a stale file is refreshed.
	if err := os.WriteFile(p, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errb = runCLI(t, dir, "skill", "path"); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if strings.TrimSpace(out) != want {
		t.Fatalf("path changed between calls: %q", out)
	}
	data, _ = os.ReadFile(p)
	if string(data) != agentskill.ReviewingWithGG.SkillFile() {
		t.Fatal("a file whose marker version differs must be rewritten")
	}
}

func TestSkillPathUsingGG(t *testing.T) {
	cache := t.TempDir()
	old := SkillCacheDir
	SkillCacheDir = cache
	t.Cleanup(func() { SkillCacheDir = old })

	dir := newRepoDir(t)
	code, out, errb := runCLI(t, dir, "skill", "path", "using-gg")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	want := filepath.Join(cache, "gg", "skills", "using-gg", "SKILL.md")
	if strings.TrimSpace(out) != want {
		t.Fatalf("path = %q, want %q", out, want)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != agentskill.UsingGG.SkillFile() {
		t.Fatal("the written file must be the embedded using-gg skill")
	}
}

// TestSkillPathRewritesADifferentMarkerVersion covers the direction a
// "refresh when outdated" check would miss: a cache file stamped NEWER than
// the binary (a v99 copy left by a newer gg) must still be replaced by what
// THIS binary carries, or the agent reads a skill describing verbs this
// binary does not have.
func TestSkillPathRewritesADifferentMarkerVersion(t *testing.T) {
	cache := t.TempDir()
	old := SkillCacheDir
	SkillCacheDir = cache
	t.Cleanup(func() { SkillCacheDir = old })

	path := filepath.Join(cache, "gg", "skills", "reviewing-with-gg", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	seeded := "<!-- gg:reviewing-with-gg:v99 -->\n\nfrom a newer gg\n"
	if err := os.WriteFile(path, []byte(seeded), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := newRepoDir(t)
	code, out, errb := runCLI(t, dir, "skill", "path")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if strings.TrimSpace(out) != path {
		t.Fatalf("path = %q, want %q", out, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != agentskill.ReviewingWithGG.SkillFile() {
		t.Error("a v99 cache copy must be rewritten to this binary's skill")
	}
	if got := agentskill.ReviewingWithGG.InstalledVersion(data); got != agentskill.ReviewVersion {
		t.Errorf("marker version = %d, want %d", got, agentskill.ReviewVersion)
	}
}

func TestSkillPathHelpPrintsUsage(t *testing.T) {
	cache := t.TempDir()
	old := SkillCacheDir
	SkillCacheDir = cache
	t.Cleanup(func() { SkillCacheDir = old })

	dir := newRepoDir(t)
	for _, flagArg := range []string{"-h", "--help"} {
		code, out, errb := runCLI(t, dir, "skill", "path", flagArg)
		if code != 2 { // every sibling verb's -h exits 2
			t.Errorf("%s: exit=%d, want 2", flagArg, code)
		}
		if out != "" {
			t.Errorf("%s: usage must not go to stdout, got %q", flagArg, out)
		}
		for _, want := range []string{"usage: gg skill path", "default is review", "rewritten"} {
			if !strings.Contains(errb, want) {
				t.Errorf("%s: usage missing %q, got:\n%s", flagArg, want, errb)
			}
		}
	}
}

func TestSkillIsAKnownCommand(t *testing.T) {
	t.Parallel()
	if !IsCommand("skill") {
		t.Error(`"skill" must be in the commands map (cmd/gg routes CLI vs TUI on IsCommand)`)
	}
}

func TestSkillUsageErrors(t *testing.T) {
	cache := t.TempDir()
	old := SkillCacheDir
	SkillCacheDir = cache
	t.Cleanup(func() { SkillCacheDir = old })

	dir := newRepoDir(t)
	for _, args := range [][]string{
		{"skill"},
		{"skill", "path", "bogus"},
		{"skill", "frobnicate"},
	} {
		if code, _, errb := runCLI(t, dir, args...); code != 2 {
			t.Errorf("%v: exit=%d stderr=%s, want 2", args, code, errb)
		}
	}
	// A usage error must never leave a materialised file behind.
	if entries, err := os.ReadDir(filepath.Join(cache, "gg", "skills")); err == nil && len(entries) > 0 {
		t.Errorf("usage errors wrote %d skill dirs into the cache", len(entries))
	}
}
