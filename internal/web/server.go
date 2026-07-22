// Package web is gg's read-only browser frontend probe: an embedded HTTP
// server exposing the domain read-model as JSON plus a static single-page
// UI. Domain-only frontend — it reaches git through internal/domain, never
// internal/git (archtest-guarded).
package web

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
)

// Server serves the probe's JSON API and static assets for one repository.
type Server struct {
	svc *domain.Service

	mu   sync.Mutex
	feed *domain.CommitFeed

	// page-size overrides applied to the feed when > 0 (test seam).
	pageInitial int
	pageBatch   int
}

func New(svc *domain.Service) *Server { return &Server{svc: svc} }

// Handler returns the full route mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	mux.HandleFunc("GET /api/repo", s.handleRepo)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/commits", s.handleCommits)
	mux.HandleFunc("GET /api/commit/{sha}", s.handleCommitFiles)
	mux.HandleFunc("GET /api/diff", s.handleDiff)
	return hostGuard(mux)
}

func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) {
	top, err := s.svc.TopLevel(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	branch, err := s.svc.CurrentBranch(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"name":     filepath.Base(top),
		"worktree": top,
		"branch":   branch,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// hostGuard rejects requests whose Host header does not name a loopback
// address. The listener already binds loopback only, but without this a
// malicious web page could reach the server via DNS rebinding (the TCP
// connection is to 127.0.0.1 while the Host header is an attacker domain)
// and read repository contents. Reuses isLoopbackHost (serve.go).
func hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !isLoopbackHost(host) {
			writeErr(w, http.StatusForbidden, errors.New("forbidden host"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isGitArgSafe reports whether s is safe to pass to a git verb as a
// positional revision/path. Untrusted HTTP params flow into git argv, so a
// value git would parse as an option (leading dash) is rejected before any
// verb sees it.
func isGitArgSafe(s string) bool {
	return s != "" && !strings.HasPrefix(s, "-")
}
