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
