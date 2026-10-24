package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/observ"
)

// These tests swap the process-global span sink: never t.Parallel().

// opLogFixture isolates the config and state homes and answers the log path.
func opLogFixture(t *testing.T) (global, logPath string) {
	t.Helper()
	global = isolateGlobal(t)
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Cleanup(func() { observ.SetSpanSink(nil) })
	return global, webOpLogPath()
}

func emitMarker(name string) {
	observ.EmitSpan(observ.Span{Name: name, Start: time.Now()})
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// A standalone gg web honours [debug] log_operations at startup, as tui.Run
// does: the run-delimiting marker, then every span.
func TestOpLogStandaloneFromConfig(t *testing.T) {
	global, logPath := opLogFixture(t)
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte("[debug]\nlog_operations = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(domain.Open(newRepoDir(t, 1)))
	srv.initOpLog(context.Background())
	emitMarker("f3-startup-span")
	got := readLog(t, logPath)
	if !strings.Contains(got, `"gg log enabled"`) || !strings.Contains(got, "version=") {
		t.Errorf("no run marker in %s:\n%s", logPath, got)
	}
	if !strings.Contains(got, "f3-startup-span") {
		t.Errorf("span after startup not logged:\n%s", got)
	}
}

// The Settings toggle flips a standalone log live, both ways.
func TestOpLogToggleLive(t *testing.T) {
	_, logPath := opLogFixture(t)
	srv := New(domain.Open(newRepoDir(t, 1)))
	srv.initOpLog(context.Background()) // config off: nothing yet
	emitMarker("f3-before-on")
	ts := serve(t, srv)
	if code := postJSON(t, ts, "/api/settings", `{"op_log":true}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("POST on = %d", code)
	}
	emitMarker("f3-while-on")
	if code := postJSON(t, ts, "/api/settings", `{"op_log":false}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("POST off = %d", code)
	}
	emitMarker("f3-after-off")
	got := readLog(t, logPath)
	if strings.Contains(got, "f3-before-on") {
		t.Errorf("logged before the toggle:\n%s", got)
	}
	if !strings.Contains(got, "f3-while-on") {
		t.Errorf("toggle on did not log:\n%s", got)
	}
	if strings.Contains(got, "f3-after-off") {
		t.Errorf("toggle off still logs:\n%s", got)
	}
}

// A hosted page never touches the sink: the TUI's opLog owns it.
func TestOpLogHostedUntouched(t *testing.T) {
	_, logPath := opLogFixture(t)
	srv := New(domain.Open(newRepoDir(t, 1))) // no initOpLog: what a TUI-hosted server is
	ts := serve(t, srv)
	if code := postJSON(t, ts, "/api/settings", `{"op_log":true}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("POST = %d", code)
	}
	emitMarker("f3-hosted")
	if got := readLog(t, logPath); got != "" {
		t.Errorf("hosted page wrote the log:\n%s", got)
	}
}

// The Settings row says whether THIS server writes the log: a standalone one
// does (no "(TUI)" caption), a hosted one leaves it to its terminal.
func TestOpLogHereInSettings(t *testing.T) {
	opLogFixture(t)
	for _, standalone := range []bool{true, false} {
		srv := New(domain.Open(newRepoDir(t, 1)))
		if standalone {
			srv.initOpLog(context.Background())
		}
		var got struct {
			Here bool `json:"op_log_here"`
		}
		if code := getJSON(t, serve(t, srv), "/api/settings", &got); code != http.StatusOK {
			t.Fatalf("GET = %d", code)
		}
		if got.Here != standalone {
			t.Errorf("standalone=%v: op_log_here = %v", standalone, got.Here)
		}
	}
	js := readStatic(t, "settings.js")
	if !strings.Contains(js, "d.op_log_here") {
		t.Error("settings.js: the operation log's (TUI) caption ignores op_log_here")
	}
}
