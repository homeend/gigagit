package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

type stageRequest struct {
	Paths   []string `json:"paths"`
	Unstage bool     `json:"unstage"`
	All     bool     `json:"all"`
}

func (s *Server) handleStage(w http.ResponseWriter, r *http.Request) {
	var req stageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	// Mirror engine.Stage's contract at the HTTP boundary so the caller gets
	// a 400, not a 500 from the op.
	if req.All && (req.Unstage || len(req.Paths) > 0) {
		writeErr(w, http.StatusBadRequest, errors.New("all is mutually exclusive with paths/unstage"))
		return
	}
	if !req.All && len(req.Paths) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("paths required unless all"))
		return
	}
	for _, p := range req.Paths {
		if !isGitArgSafe(p) {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid path %q", p))
			return
		}
	}
	if _, err := runOp(r.Context(), s.svc, engine.Stage{Paths: req.Paths, All: req.All, Unstage: req.Unstage}); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.writeStatus(w, r) // success response = fresh status (one round-trip)
}

// noDecider fails loud on any decision request: the web probe only runs ops
// that never fork (a fork reaching it is a programming error, surfaced as a
// 500 rather than a wedge).
type noDecider struct{}

func (noDecider) Decide(_ context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error) {
	return engine.DecisionResponse{}, fmt.Errorf(
		"unexpected decision %q (options: %s)", req.ID, strings.Join(req.Options, ", "))
}

// runOp executes op via domain.Execute, draining events (the HTTP reply
// carries the outcome; per-line progress has no channel here). The MCP
// frontend's runOp pattern (internal/mcp/decider.go).
func runOp(ctx context.Context, svc *domain.Service, op engine.Operation) (engine.Result, error) {
	events := make(chan engine.Event, 32)
	done := make(chan struct{})
	var (
		res engine.Result
		err error
	)
	go func() {
		res, err = svc.Execute(ctx, op, events, noDecider{})
		close(events)
		close(done)
	}()
	for range events {
	}
	<-done
	return res, err
}
