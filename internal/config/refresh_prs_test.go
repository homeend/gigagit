package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPRsSecondsDefaultsTo300(t *testing.T) {
	t.Parallel()
	if got := (RefreshConfig{}).PRsSeconds(); got != 300 {
		t.Fatalf("unset prs = %d, want 300", got)
	}
	zero, neg := 0, -5
	if got := (RefreshConfig{PRs: &zero}).PRsSeconds(); got != 0 {
		t.Fatalf("prs = 0 must mean off, got %d", got)
	}
	if got := (RefreshConfig{PRs: &neg}).PRsSeconds(); got != 0 {
		t.Fatalf("a negative prs must read as off, got %d", got)
	}
}

func TestOverlayRefreshPRsZeroWins(t *testing.T) {
	t.Parallel()
	ten, zero := 10, 0
	dst := RefreshConfig{PRs: &ten}
	overlayRefresh(&dst, RefreshConfig{PRs: &zero})
	if dst.PRsSeconds() != 0 {
		t.Fatalf("an explicit prs = 0 in a higher layer must turn the poll off")
	}
	overlayRefresh(&dst, RefreshConfig{}) // an unset layer leaves it alone
	if dst.PRsSeconds() != 0 {
		t.Fatalf("an unset layer must not reset prs")
	}
	if dst.PRs == &zero {
		t.Fatalf("the overlay must copy the value, not alias the source layer")
	}
}

func TestLoadRefreshPRsLayers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	repo := filepath.Join(dir, "repo.toml")
	if err := os.WriteFile(global, []byte("[refresh]\nprs = 60\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(global, filepath.Join(dir, "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Refresh.PRsSeconds(); got != 60 {
		t.Fatalf("global prs = %d, want 60", got)
	}
	if err := os.WriteFile(repo, []byte("[refresh]\nprs = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Refresh.PRsSeconds(); got != 0 {
		t.Fatalf("repo prs = 0 over global 60 = %d, want 0 (off)", got)
	}
	cfg, err = Load(filepath.Join(dir, "none.toml"), filepath.Join(dir, "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Refresh.PRsSeconds(); got != 300 {
		t.Fatalf("no layer sets prs = %d, want the 300 default", got)
	}
}
