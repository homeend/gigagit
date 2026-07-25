// Package web is gg's browser frontend probe: an embedded HTTP server
// exposing the domain read-model as JSON plus a static single-page UI.
// Domain-only frontend — it reaches git through internal/domain, never
// internal/git (archtest-guarded). Mutating endpoints (stage) run engine
// ops through domain.Execute behind writeGuard.
package web

import (
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// Server serves the probe's JSON API and static assets for one repository.
type Server struct {
	// svc is swappable at runtime (POST /api/reroot) — handlers read it
	// once per request via service(), never cache it.
	svc atomic.Pointer[domain.Service]

	mu   sync.Mutex
	feed *domain.CommitFeed

	// page-size overrides applied to the feed when > 0 (test seam).
	pageInitial int
	pageBatch   int

	// op transport (oprun.go): one live operation at a time.
	opMu          sync.Mutex
	cur           *opRun
	opSeq         int
	decideTimeout time.Duration // test seam; zero = defaultDecideTimeout

	// reposPath overrides the MRU registry location (test seam); empty =
	// repos.DefaultStatePath().
	reposPath string
}

func New(svc *domain.Service) *Server {
	s := &Server{}
	s.svc.Store(svc)
	return s
}

// service returns the current domain service. Read it once at the top of a
// handler and use the local for the whole request.
func (s *Server) service() *domain.Service { return s.svc.Load() }

// Handler returns the full route mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	mux.HandleFunc("GET /api/repo", s.handleRepo)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/branches", s.handleBranches)
	mux.HandleFunc("GET /api/worktrees", s.handleWorktrees)
	mux.HandleFunc("GET /api/tags", s.handleTags)
	mux.HandleFunc("GET /api/stashes", s.handleStashes)
	mux.HandleFunc("GET /api/commits", s.handleCommits)
	mux.HandleFunc("GET /api/commit/{sha}", s.handleCommitFiles)
	mux.HandleFunc("GET /api/diff", s.handleDiff)
	mux.HandleFunc("POST /api/stage", writeGuard(s.handleStage))
	mux.HandleFunc("GET /api/hunks", s.handleHunks)
	mux.HandleFunc("POST /api/stage-hunks", writeGuard(s.handleStageHunks))
	mux.HandleFunc("POST /api/op", writeGuard(s.handleOpStart))
	mux.HandleFunc("GET /api/op/{id}/events", s.handleOpEvents)
	mux.HandleFunc("POST /api/op/{id}/decide", writeGuard(s.handleOpDecide))
	mux.HandleFunc("POST /api/reroot", writeGuard(s.handleReroot))
	return hostGuard(mux)
}

func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) {
	writeRepoInfo(w, r, s.service())
}

// writeRepoInfo writes the repo-identity payload for svc — shared by GET
// /api/repo and the POST /api/reroot success response.
func writeRepoInfo(w http.ResponseWriter, r *http.Request, svc *domain.Service) {
	top, err := svc.TopLevel(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	branch, err := svc.CurrentBranch(r.Context())
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

// writeGuard hardens mutating endpoints against cross-site requests. A
// cross-site HTML form cannot send application/json without a CORS
// preflight (which this server never answers), and browsers always attach
// an Origin header to cross-site POSTs — so requiring the JSON content
// type plus a loopback Origin (when one is present; curl sends none)
// closes CSRF on top of hostGuard's DNS-rebinding check.
func writeGuard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mt != "application/json" {
			writeErr(w, http.StatusUnsupportedMediaType, errors.New("Content-Type must be application/json"))
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !isLoopbackHost(u.Hostname()) {
				writeErr(w, http.StatusForbidden, errors.New("forbidden origin"))
				return
			}
		}
		next(w, r)
	}
}

// isGitArgSafe reports whether s is safe to pass to a git verb as a
// positional revision/path. Untrusted HTTP params flow into git argv, so a
// value git would parse as an option (leading dash) is rejected before any
// verb sees it.
func isGitArgSafe(s string) bool {
	return s != "" && !strings.HasPrefix(s, "-")
}
