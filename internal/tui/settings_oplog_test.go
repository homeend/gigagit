package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

func opLogMenuIndex(t *testing.T) int {
	t.Helper()
	for i := range settingsMenu {
		if settingsMenu[i] == settingsMenuOpLog {
			return i
		}
	}
	t.Fatal("operation-log entry missing from settings menu")
	return -1
}

// TestSettingsOpLogTogglePersistsAndLabels drives the , Settings toggle: it
// creates the log file, mirrors spans, persists the choice to the global config,
// and the menu label reflects state + filename — then toggling back reverses all
// of it.
func TestSettingsOpLogTogglePersistsAndLabels(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() { observ.SetSpanSink(nil) })

	m := New(domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()}))
	idx := opLogMenuIndex(t)

	if m.opLog.on {
		t.Fatal("operation log should start off")
	}
	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "off") {
		t.Fatalf("off-state label missing 'off': %q", got)
	}

	// Enable.
	m = m.toggleOpLog()
	if !m.opLog.on {
		t.Fatal("toggle should enable the log")
	}
	if _, err := os.Stat(m.opLog.path); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	cfg, err := config.Load(config.DefaultGlobalPath(), "")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Debug.LogOperations {
		t.Fatal("enabling should persist log_operations=true to global config")
	}
	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "on") || !strings.Contains(got, m.opLog.path) {
		t.Fatalf("on-state label missing state/path: %q", got)
	}

	// Disable.
	m = m.toggleOpLog()
	if m.opLog.on {
		t.Fatal("toggle should disable the log")
	}
	cfg, _ = config.Load(config.DefaultGlobalPath(), "")
	if cfg.Debug.LogOperations {
		t.Fatal("disabling should persist log_operations=false to global config")
	}
}
