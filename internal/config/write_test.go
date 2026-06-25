package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetGlobalDebugLogOperationsUncommentsTemplateLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(Template()), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetGlobalDebugLogOperations(path, true); err != nil {
		t.Fatalf("set: %v", err)
	}

	// Round-trips through Load: the value is now active.
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Debug.LogOperations {
		t.Fatal("expected log_operations=true after enabling")
	}

	// Comments and other sections survive (non-destructive edit).
	data, _ := os.ReadFile(path)
	body := string(data)
	if !strings.Contains(body, "[worktree]") || !strings.Contains(body, "[ui]") {
		t.Fatalf("other sections lost:\n%s", body)
	}
	if !strings.Contains(body, "# gg configuration") {
		t.Fatalf("header comment lost:\n%s", body)
	}

	// Toggling back off is also honored.
	if err := SetGlobalDebugLogOperations(path, false); err != nil {
		t.Fatalf("unset: %v", err)
	}
	cfg, _ = Load(path, "")
	if cfg.Debug.LogOperations {
		t.Fatal("expected log_operations=false after disabling")
	}
}

func TestSetGlobalDebugLogOperationsCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := SetGlobalDebugLogOperations(path, true); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Debug.LogOperations {
		t.Fatal("expected log_operations=true in freshly created file")
	}
}

func TestDebugOverlayRepoWins(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "config.toml")
	repo := filepath.Join(dir, ".gg.toml")
	os.WriteFile(global, []byte("[debug]\nlog_operations = true\n"), 0o644)
	os.WriteFile(repo, []byte(""), 0o644)

	cfg, err := Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	// Inverted polarity: an empty repo layer cannot reset the global's true.
	if !cfg.Debug.LogOperations {
		t.Fatal("global true should survive an empty repo layer")
	}
}
