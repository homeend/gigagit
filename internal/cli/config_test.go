package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigInitRepoWritesTemplate(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if rc := cmdConfig(dir, []string{"init", "--repo"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gg.toml"))
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if !strings.Contains(string(data), "[ui]") || !strings.Contains(string(data), "reflog_limit") {
		t.Fatalf("template content missing:\n%s", data)
	}
}

func TestConfigInitRefusesExistingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(path, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if rc := cmdConfig(dir, []string{"init", "--repo"}, &out, &errOut); rc == 0 {
		t.Fatal("must refuse to overwrite without --force")
	}
	if !strings.Contains(errOut.String(), path) {
		t.Fatalf("refuse message must name the path, got %q", errOut.String())
	}
	if b, _ := os.ReadFile(path); string(b) != "# mine\n" {
		t.Fatal("existing file must be left untouched")
	}
}

func TestConfigInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(path, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if rc := cmdConfig(dir, []string{"init", "--repo", "--force"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "[ui]") {
		t.Fatal("--force must overwrite with the template")
	}
}

func TestConfigInitGlobalUsesXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	var out, errOut bytes.Buffer
	if rc := cmdConfig(t.TempDir(), []string{"init", "--global"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(home, "gg", "config.toml")); err != nil {
		t.Fatalf("global config not written under XDG dir: %v", err)
	}
}

func TestConfigInitRequiresExactlyOneScope(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if rc := cmdConfig(dir, []string{"init"}, &out, &errOut); rc == 0 {
		t.Fatal("neither --repo nor --global must error")
	}
	if rc := cmdConfig(dir, []string{"init", "--repo", "--global"}, &out, &errOut); rc == 0 {
		t.Fatal("both --repo and --global must error")
	}
}
