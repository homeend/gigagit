// Package mcp implements gigagit's MCP (Model Context Protocol) frontend: a
// stdio server exposing gg's NON-git value — the TUI session snapshot,
// bookmarks, shelves, gg-specific compare and export — to AI agents. Stage 1
// is the safe surface only (reads, compares, export-to-a-directory); it never
// mutates the repository. A domain-only frontend like internal/cli: it never
// imports internal/git (archtest-enforced).
package mcp

import (
	"context"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// Server wires gg's domain service to the MCP tool surface.
type Server struct {
	svc       *domain.Service
	commonDir string // absolute git common dir; "" when repo resolution failed
	worktree  string // worktree top-level; "" when repo resolution failed
	repoErr   error  // startup repo-resolution failure, surfaced per-tool

	// steerDir is this worktree's live-steering inbox; a mutating note tool
	// posts a best-effort `reload notes` into it so the human's open window
	// updates. "" = no inbox (no state home, or repo resolution failed).
	// Overridable by tests.
	steerDir string
}

// New resolves the repo identity once. A failure is remembered, not fatal:
// the server still starts (a server that dies at startup shows up as an
// opaque client-side failure) and every tool reports the problem clearly.
func New(svc *domain.Service) *Server {
	s := &Server{svc: svc}
	ctx := context.Background()
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		s.repoErr = fmt.Errorf("not a git repository (run gg mcp from inside a repo): %v", err)
		return s
	}
	s.commonDir = cd
	if top, err := svc.TopLevel(ctx); err == nil {
		s.worktree = top
		s.steerDir = config.SessionSteerDir(cd, top)
	}
	// gg mcp is long-lived, so it does the same note housekeeping a TUI does:
	// apply the configured [notes] budget, then sweep once in the background.
	// Best-effort — a config that will not load must never stop the server.
	if cfg, cerr := svc.EffectiveConfig(ctx); cerr == nil {
		svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
	}
	svc.StartNotesSweep()
	return s
}

func (s *Server) repoInfo() RepoInfo {
	return RepoInfo{CommonDir: s.commonDir, Worktree: s.worktree}
}

func (s *Server) repoCheck() error { return s.repoErr }

// sdkServer builds the SDK server with every stage-1 tool registered.
func (s *Server) sdkServer() *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "gg", Version: buildinfo.Version}, nil)
	s.registerStateTool(srv)
	s.registerBookmarkTools(srv)
	s.registerShelfTools(srv)
	s.registerCompareTools(srv)
	s.registerLinkTools(srv)
	s.registerSavedListTools(srv)
	s.registerExportTool(srv)
	s.registerCherryPickTool(srv)
	s.registerWriteTool(srv)
	s.registerNoteTools(srv)
	return srv
}

// Serve runs the MCP server over stdio until ctx ends or the client closes.
// workdir resolves the repo like the CLI does (the process cwd for gg mcp).
//
// A Required feature that cannot be satisfied refuses the server outright
// rather than starting it: MCP never prompts and never decides mid-flight
// (a standing ruling — see the Decider contract in internal/engine), so a
// gated Required feature has no in-protocol way to ask for consent or even
// explain itself per-tool the way ErrFeatureDisabled does for Optional ones.
// Refusing to start, with the reason on stderr, is the correct MCP analogue
// of the TUI's pre-launch gate.
func Serve(ctx context.Context, workdir string) error {
	svc := domain.Open(workdir)
	if err := svc.PreflightRequired(ctx); err != nil {
		return err
	}
	return New(svc).sdkServer().Run(ctx, &sdk.StdioTransport{})
}
