package web

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

// The open workspace as the page sees it (spec 2026-10-31-web-workspaces §2).
// The page never holds what is open: an owner pushes identity through
// SetWorkspace and is asked through the actor. The terminal is the owner under
// gg and gg --web; workspace_owner.go is under standalone gg web.

// wsState is the owner's latest word. Guarded by Server.mu.
type wsState struct {
	info     *domain.WorkspaceInfo
	question *domain.WorkspaceQuestion
	// changes counts a member's changes per checkout. It outlives the live hub:
	// a hello re-reads only the members whose counter moved, so an event lost
	// while a switch replaced the hub is not lost.
	changes map[model.CheckoutKey]int
}

type wsMemberWire struct {
	Path       string `json:"path"`
	Label      string `json:"label"`
	Missing    bool   `json:"missing"`
	LastViewed string `json:"last_viewed"`
	Changes    int    `json:"changes"`
}

type wsOpenWire struct {
	Name         string         `json:"name"`
	Live         int            `json:"live"`
	SettingsRepo string         `json:"settings_repo"`
	Members      []wsMemberWire `json:"members"`
}

type wsQuestionRowWire struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

type wsQuestionWire struct {
	Repo string              `json:"repo"`
	Rows []wsQuestionRowWire `json:"rows"`
}

type wsDefinedMemberWire struct {
	Path    string `json:"path"`
	Label   string `json:"label"`
	Missing bool   `json:"missing"`
}

// wsDefinedWire is one workspaces.toml entry as it would open.
type wsDefinedWire struct {
	Name         string                `json:"name"`
	SettingsRepo string                `json:"settings_repo"`
	Repos        []wsDefinedMemberWire `json:"repos"`
}

// SetWorkspace is the owner saying what is open; nil = no workspace.
func (s *Server) SetWorkspace(info *domain.WorkspaceInfo) {
	s.mu.Lock()
	s.ws.info = info
	s.mu.Unlock()
	s.fanOutWorkspace("workspace", "")
}

// SetWorkspaceQuestion holds the pending launch question; nil = none.
func (s *Server) SetWorkspaceQuestion(q *domain.WorkspaceQuestion) {
	s.mu.Lock()
	s.ws.question = q
	s.mu.Unlock()
	s.fanOutWorkspace("workspace", "")
}

// MemberChanged is the owner saying a sleeping member's rows moved. It goes
// through fanOut, not emit: emit's gate drops events during an operation, and
// a sleeping member changing has nothing to do with the operation on screen.
func (s *Server) MemberChanged(path string) {
	s.mu.Lock()
	if s.ws.changes == nil {
		s.ws.changes = map[model.CheckoutKey]int{}
	}
	s.ws.changes[model.KeyOf(path)]++
	s.mu.Unlock()
	s.fanOutWorkspace("workspace_member", path)
}

func (s *Server) fanOutWorkspace(reason, path string) {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: reason, Path: path})
	}
}

func (s *Server) workspaceInfo() *domain.WorkspaceInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ws.info
}

func (s *Server) handleWorkspaceGet(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	info, q := s.ws.info, s.ws.question
	var open *wsOpenWire
	if info != nil {
		open = &wsOpenWire{Name: info.Name, Live: info.Live, SettingsRepo: info.SettingsRepo, Members: make([]wsMemberWire, 0, len(info.Members))}
		for _, m := range info.Members {
			open.Members = append(open.Members, wsMemberWire{
				Path: m.Path, Label: m.Label, Missing: m.Missing, LastViewed: m.LastViewed,
				Changes: s.ws.changes[model.KeyOf(m.Path)],
			})
		}
	}
	s.mu.Unlock()
	var question *wsQuestionWire
	if q != nil {
		question = &wsQuestionWire{Repo: q.Repo, Rows: make([]wsQuestionRowWire, 0, len(q.Rows))}
		for _, r := range q.Rows {
			question.Rows = append(question.Rows, wsQuestionRowWire{Name: r.Name, Label: r.Label})
		}
	}
	writeJSON(w, map[string]any{"open": open, "question": question, "defined": definedWorkspaces(s.workspacesPath())})
}

func (s *Server) workspacesPath() string { return domain.WorkspacesPath(s.reposStatePath()) }

// definedWorkspaces reads workspaces.toml for R and the Workspaces window.
func definedWorkspaces(path string) []wsDefinedWire {
	entries := workspace.NormalizeEntries(workspace.Load(path))
	out := make([]wsDefinedWire, 0, len(entries))
	for _, e := range entries {
		names := make([]string, len(e.Repos))
		for i, p := range e.Repos {
			names[i] = filepath.Base(p)
		}
		labels := workspace.HeadingLabels(names, e.Repos)
		d := wsDefinedWire{Name: e.Name, SettingsRepo: e.SettingsRepo, Repos: make([]wsDefinedMemberWire, 0, len(e.Repos))}
		for i, p := range e.Repos {
			d.Repos = append(d.Repos, wsDefinedMemberWire{Path: p, Label: labels[i], Missing: statErr(p) != nil})
		}
		out = append(out, d)
	}
	return out
}

// handleWorkspaceMember serves one member's rows in today's wire shapes, built
// by the code behind /api/worktrees and /api/branches over the member's service.
func (s *Server) handleWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	m, ok := s.workspaceMember(r.URL.Query().Get("path"))
	if !ok {
		writeErr(w, http.StatusBadRequest, errors.New("not a member of the open workspace"))
		return
	}
	if _, err := os.Stat(m.Path); err != nil {
		writeJSON(w, map[string]any{"missing": true})
		return
	}
	svc := m.Svc
	if svc == nil {
		svc = s.open(m.Path) // pushed as missing, and back since: the stat outranks the push
	}
	ctx := readCtx(r)
	wts, err := s.worktreeRows(ctx, svc)
	if err != nil {
		writeJSON(w, map[string]any{"missing": false, "error": err.Error()})
		return
	}
	brs, filter, err := s.branchRows(ctx, svc)
	if err != nil {
		writeJSON(w, map[string]any{"missing": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"missing": false, "worktrees": wts, "branches": brs, "filter": filter})
}

// workspaceMember resolves a wire path against the member allowlist.
func (s *Server) workspaceMember(path string) (domain.WorkspaceMember, bool) {
	info := s.workspaceInfo()
	if info == nil || path == "" {
		return domain.WorkspaceMember{}, false
	}
	for _, m := range info.Members {
		if model.SamePath(m.Path, path) {
			return m, true
		}
	}
	return domain.WorkspaceMember{}, false
}

// memberSwitch reports a swap from one member to another: the page keeps its
// place and re-reads the repository-wide lists. No workspace = no git call.
func (s *Server) memberSwitch(ctx context.Context, from, to *domain.Service) bool {
	info := s.workspaceInfo()
	if info == nil {
		return false
	}
	a, err := from.RepoMainPath(ctx)
	if err != nil {
		return false
	}
	b, err := to.RepoMainPath(ctx)
	if err != nil || model.SamePath(a, b) {
		return false
	}
	_, fromOK := s.workspaceMember(a)
	_, toOK := s.workspaceMember(b)
	return fromOK && toOK
}
