package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportShelfCommitToDir(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	dest := filepath.Join(t.TempDir(), "out")
	out := e.call(t, "gg_export", map[string]any{
		"shelf": ce.ID,
		"dir":   dest,
	})
	if out["dir"] != dest {
		t.Fatalf("dir = %v", out["dir"])
	}
	if int(out["count"].(float64)) != 1 {
		t.Fatalf("count = %v", out["count"])
	}
	data, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil || string(data) != "hello\nworld\n" {
		t.Fatalf("exported file: %q err=%v", data, err)
	}
}

func TestExportBookmarkDefaultDir(t *testing.T) {
	e := newTestEnv(t)
	b := seedBookmark(t, e)
	out := e.call(t, "gg_export", map[string]any{"bookmark": b.ID})
	dir, _ := out["dir"].(string)
	if dir == "" {
		t.Fatalf("no default dir: %v", out)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	files := out["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	if _, err := os.Stat(filepath.Join(dir, files[0].(string))); err != nil {
		t.Fatalf("exported file missing: %v", err)
	}
}

func TestExportExistingDirRefusedWithoutOverwrite(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	dest := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	msg := e.callErr(t, "gg_export", map[string]any{"shelf": ce.ID, "dir": dest})
	if !strings.Contains(msg, "overwrite:true") {
		t.Fatalf("refusal must name the fix: %s", msg)
	}
	// and with overwrite it succeeds
	out := e.call(t, "gg_export", map[string]any{"shelf": ce.ID, "dir": dest, "overwrite": true})
	if int(out["count"].(float64)) != 1 {
		t.Fatalf("overwrite export failed: %v", out)
	}
}

func TestExportNeedsExactlyOneSource(t *testing.T) {
	e := newTestEnv(t)
	msg := e.callErr(t, "gg_export", map[string]any{})
	if !strings.Contains(msg, "exactly one") {
		t.Fatalf("error = %s", msg)
	}
	msg = e.callErr(t, "gg_export", map[string]any{"bookmark": "x", "shelf": "y"})
	if !strings.Contains(msg, "exactly one") {
		t.Fatalf("error = %s", msg)
	}
}
