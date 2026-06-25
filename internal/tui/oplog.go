package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repos"
)

// opLog owns the operation-log file plus the process-wide span-sink lifecycle.
// It is a pointer field on Model so the open handle and on/off state survive the
// value-receiver copies. When on, every recorded span — each engine operation
// and each git invocation, redacted — is appended as one JSON line, leaving a
// trace of a hung or slow op.
type opLog struct {
	path string   // operations.log location (shown in the Settings menu)
	on   bool     // currently mirroring spans to the file
	file *os.File // open append handle while on; nil when off
}

// newOpLog resolves the log location (beside the repo registry in the gg state
// dir) without opening anything.
func newOpLog() *opLog { return &opLog{path: defaultOpLogPath()} }

// defaultOpLogPath puts the log beside the repo registry in the gg state dir,
// reusing its platform-appropriate resolution. "" if no home dir exists.
func defaultOpLogPath() string {
	sp := repos.DefaultStatePath()
	if sp == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sp), "operations.log")
}

// enable opens the log for appending, routes every span there, and writes a
// run-delimiting marker span. A no-op when already on.
func (l *opLog) enable() error {
	if l.on {
		return nil
	}
	if l.path == "" {
		return fmt.Errorf("operation log: no state directory")
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	observ.SetSpanSink(f)
	observ.EmitSpan(observ.Span{
		Name:  "gg log enabled",
		Args:  []string{"version=" + buildinfo.Version},
		Start: time.Now(),
	})
	l.file = f
	l.on = true
	return nil
}

// disable stops mirroring (under the sink mutex, so no further write touches the
// handle) and closes the file. A no-op when already off.
func (l *opLog) disable() error {
	if !l.on {
		return nil
	}
	observ.SetSpanSink(nil)
	err := l.file.Close()
	l.file = nil
	l.on = false
	return err
}
