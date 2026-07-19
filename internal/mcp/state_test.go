package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
)

func snapshotPathFor(t *testing.T, e *testEnv) string {
	t.Helper()
	cd, err := e.svc.GitCommonDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := config.SessionSnapshotPath(cd)
	if p == "" {
		t.Fatal("no snapshot path (XDG_STATE_HOME should be set by newTestEnv)")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUIStateNoSession(t *testing.T) {
	e := newTestEnv(t)
	out := e.call(t, "gg_ui_state", nil)
	if out["session"] != nil {
		t.Fatalf("session must be null with no snapshot, got %v", out["session"])
	}
	if hint, _ := out["hint"].(string); !strings.Contains(hint, "no gg TUI session") {
		t.Fatalf("hint = %v", out["hint"])
	}
}

func TestUIStateReadsSnapshot(t *testing.T) {
	e := newTestEnv(t)
	p := snapshotPathFor(t, e)
	body := `{"version":1,"pid":42,"focus":{"panel":"commits"},"marked_commits":["abc"]}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_ui_state", nil)
	sess, ok := out["session"].(map[string]any)
	if !ok {
		t.Fatalf("session = %v", out["session"])
	}
	if sess["pid"].(float64) != 42 {
		t.Fatalf("session.pid = %v", sess["pid"])
	}
	if focus := sess["focus"].(map[string]any); focus["panel"] != "commits" {
		t.Fatalf("session.focus = %v", sess["focus"])
	}
}

func TestUIStateVersionTooNew(t *testing.T) {
	e := newTestEnv(t)
	p := snapshotPathFor(t, e)
	if err := os.WriteFile(p, []byte(`{"version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := e.callErr(t, "gg_ui_state", nil)
	if !strings.Contains(msg, "newer") {
		t.Fatalf("expected version-too-new error, got: %s", msg)
	}
}

func TestUIStateCorruptSnapshot(t *testing.T) {
	e := newTestEnv(t)
	p := snapshotPathFor(t, e)
	if err := os.WriteFile(p, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := e.callErr(t, "gg_ui_state", nil)
	if !strings.Contains(msg, "unreadable") {
		t.Fatalf("expected unreadable error, got: %s", msg)
	}
}
