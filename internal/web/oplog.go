package web

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/observ"
)

// webOpLog is a standalone gg web's operation log — the TUI's opLog
// (internal/tui/oplog.go) for a server with no terminal: while on, every
// recorded span (each engine operation and git invocation, redacted) is one
// JSON line in <state>/operations.log, rolled over weekly. A page the TUI
// hosts has none: the span sink is process-global and the TUI's opLog owns
// it, so a hosted Settings toggle stays a config write.
type webOpLog struct {
	mu sync.Mutex
	on bool
}

// initOpLog gives a standalone server its log and honours [debug]
// log_operations at startup, as tui.Run does. Serve calls it; a hosted
// server never does.
func (s *Server) initOpLog(ctx context.Context) {
	s.opLog = &webOpLog{}
	if cfg, err := s.effectiveConfig(ctx, s.service()); err == nil && cfg.Debug.LogOperations {
		_ = s.opLog.set(true) // best-effort, like the TUI's startup
	}
}

// set turns the log on or off. On opens the file, routes every span there and
// writes the run-delimiting marker; off clears the sink, which closes the
// file (SetSpanSink owns that — closing here too would double-close).
func (l *webOpLog) set(on bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if on == l.on {
		return nil
	}
	if !on {
		observ.SetSpanSink(nil)
		l.on = false
		return nil
	}
	path := webOpLogPath()
	if path == "" {
		return errors.New("operation log: no state directory")
	}
	f, err := observ.OpenWeeklyLog(path)
	if err != nil {
		return err
	}
	observ.SetSpanSink(f)
	observ.EmitSpan(observ.Span{Name: "gg log enabled", Args: []string{"version=" + buildinfo.Version}, Start: time.Now()})
	l.on = true
	return nil
}
