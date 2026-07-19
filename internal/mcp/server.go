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
	"github.com/homeend/gigagit/internal/domain"
)

// Server wires gg's domain service to the MCP tool surface.
type Server struct {
	svc       *domain.Service
	commonDir string // absolute git common dir; "" when repo resolution failed
	worktree  string // worktree top-level; "" when repo resolution failed
	repoErr   error  // startup repo-resolution failure, surfaced per-tool
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
	}
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
	s.registerExportTool(srv)
	s.registerCherryPickTool(srv)
	s.registerWriteTool(srv)
	return srv
}

// Serve runs the MCP server over stdio until ctx ends or the client closes.
// workdir resolves the repo like the CLI does (the process cwd for gg mcp).
func Serve(ctx context.Context, workdir string) error {
	return New(domain.Open(workdir)).sdkServer().Run(ctx, &sdk.StdioTransport{})
}
