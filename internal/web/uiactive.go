package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// One active interface (spec 2026-10-31-one-active-interface): a hosted page
// is either the active interface or blocked behind a notice. The terminal
// owns the state and pushes a copy here; this file enforces it, so a stale
// tab or a script cannot act behind the notice. A standalone server has no
// terminal and is always active.

const (
	uiSideWeb = "web"
	uiSideTUI = "tui"
)

var errTerminalActive = errors.New("the terminal UI is active in this gg")

// uiInactive reports a blocked hosted page.
func (s *Server) uiInactive() bool { return s.hosted && s.uiBlocked.Load() }

// SetUIActive records the flip, tells the tabs, and rewrites the presence so
// routing sees it now rather than at the next tick.
func (s *Server) SetUIActive(active bool) {
	s.uiBlocked.Store(!active)
	s.rewriteSteerPresence()
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "ui_active", Active: &active})
	}
}

// SetActivator installs the terminal's flip (Host.SetActivator).
func (s *Server) SetActivator(fn func(ctx context.Context, side string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activator = fn
}

func (s *Server) uiClients() int {
	if h := s.liveHubRef(); h != nil {
		return h.clients()
	}
	return 0
}

// uiBusy is why the page may not give up control now.
func (s *Server) uiBusy() string {
	if s.opInFlight() {
		return "an operation is running"
	}
	return ""
}

// uiGate refuses a blocked page's requests, all but the few the notice and
// its boot need. They do not touch git.
func (s *Server) uiGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.uiInactive() || uiAllowedWhileBlocked(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": errTerminalActive.Error(), "ui_inactive": true})
	})
}

func uiAllowedWhileBlocked(path string) bool {
	switch path {
	case "/", "/api/ping", "/api/events", "/api/ui-active", "/api/quit":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}

func (s *Server) handleUIActiveGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]bool{"active": !s.uiInactive(), "hosted": s.hosted})
}

func (s *Server) handleUIActivePost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Side string `json:"side"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Side != uiSideWeb && req.Side != uiSideTUI {
		writeErr(w, http.StatusBadRequest, errors.New("side must be web or tui"))
		return
	}
	s.mu.Lock()
	activator := s.activator
	s.mu.Unlock()
	if activator == nil {
		writeErr(w, http.StatusConflict, errors.New("this page has no terminal to switch to"))
		return
	}
	if req.Side == uiSideTUI {
		if why := s.uiBusy(); why != "" {
			writeErr(w, http.StatusConflict, errors.New(why))
			return
		}
	}
	if err := activator(r.Context(), req.Side); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
