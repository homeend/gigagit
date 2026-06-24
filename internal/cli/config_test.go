package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestConfigInitRepoWritesTemplate(t *testing.T) {
	dir := newRepoDir(t)
	svc := domain.Open(dir)
	var out, errOut bytes.Buffer
	if rc := cmdConfig(svc, dir, []string{"init", "--repo"}, &out, &errOut); rc != 0 {
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

// --repo must write at the repo TOPLEVEL, not the cwd — gg reads repo config
// from the toplevel, so writing into a subdir would be a silent no-op.
func TestConfigInitRepoWritesAtToplevelFromSubdir(t *testing.T) {
	root := newRepoDir(t)
	sub := filepath.Join(root, "sub", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	svc := domain.Open(sub)
	top, err := svc.TopLevel(context.Background())
	if err != nil {
		t.Fatalf("toplevel: %v", err)
	}
	var out, errOut bytes.Buffer
	if rc := cmdConfig(svc, sub, []string{"init", "--repo"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(top, ".gg.toml")); err != nil {
		t.Fatalf("config must be written at the toplevel %s: %v", top, err)
	}
	if _, err := os.Stat(filepath.Join(sub, ".gg.toml")); err == nil {
		t.Fatal("config must NOT be written into the subdirectory")
	}
}

func TestConfigInitRefusesExistingWithoutForce(t *testing.T) {
	dir := newRepoDir(t)
	svc := domain.Open(dir)
	path := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(path, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if rc := cmdConfig(svc, dir, []string{"init", "--repo"}, &out, &errOut); rc == 0 {
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
	dir := newRepoDir(t)
	svc := domain.Open(dir)
	path := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(path, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if rc := cmdConfig(svc, dir, []string{"init", "--repo", "--force"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "[ui]") {
		t.Fatal("--force must overwrite with the template")
	}
}

func TestConfigInitGlobalUsesXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	svc := domain.Open(t.TempDir())
	var out, errOut bytes.Buffer
	if rc := cmdConfig(svc, t.TempDir(), []string{"init", "--global"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(home, "gg", "config.toml")); err != nil {
		t.Fatalf("global config not written under XDG dir: %v", err)
	}
}

func TestConfigInitRequiresExactlyOneScope(t *testing.T) {
	dir := t.TempDir()
	svc := domain.Open(dir)
	var out, errOut bytes.Buffer
	if rc := cmdConfig(svc, dir, []string{"init"}, &out, &errOut); rc == 0 {
		t.Fatal("neither --repo nor --global must error")
	}
	if rc := cmdConfig(svc, dir, []string{"init", "--repo", "--global"}, &out, &errOut); rc == 0 {
		t.Fatal("both --repo and --global must error")
	}
}
