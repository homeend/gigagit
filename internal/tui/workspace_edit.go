package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

// The workspace edits the Workspaces popup and the page's requests share. They
// run on a workspacePopup so the file handling (the registry as it is NOW, the
// merged result, gone-elsewhere) has one home; the page's request uses one that
// is never shown (workspaceEditApply).

// applyName creates name, or renames old to it, and follows an open workspace.
// taken names the workspace already holding name; gone = old went elsewhere.
func (p *workspacePopup) applyName(m Model, old, name string) (Model, string, bool) {
	// The duplicate check runs against the file AS IT IS, inside the
	// edit: a workspace another gg created meanwhile is never replaced.
	var taken string
	gone := false
	m = p.save(m, func(ws []workspace.Entry) []workspace.Entry {
		if other, ok := workspace.Find(ws, name); ok && (old == "" || !strings.EqualFold(other.Name, old)) {
			taken = other.Name
			return ws
		}
		if old != "" {
			if _, ok := workspace.Find(ws, old); !ok {
				gone = true // renamed or deleted elsewhere meanwhile: nothing to rename
				return ws
			}
			return workspace.Rename(ws, old, name)
		}
		return workspace.Upsert(ws, workspace.Entry{Name: name})
	}, name)
	if gone {
		return p.gone(m, p.ws, old), "", true
	}
	if taken == "" && old != "" && m.ws != nil && strings.EqualFold(m.ws.name, old) {
		m.ws.name = name
	}
	return m, taken, false
}

// applyDelete removes the workspace named name; the open one is left, its
// members' running sessions asked about BEFORE the definition goes, so Cancel
// keeps it on disk.
func (p *workspacePopup) applyDelete(m Model, name string) (Model, tea.Cmd) {
	del := func(m Model) Model {
		return p.save(m, func(ws []workspace.Entry) []workspace.Entry { return workspace.Delete(ws, name) }, "")
	}
	if m.ws != nil && strings.EqualFold(m.ws.name, name) {
		live := m.liveRepo()
		var gone []*repoView
		for _, r := range m.ws.members {
			if r != live {
				gone = append(gone, r)
			}
		}
		leave := func(m Model) (Model, tea.Cmd) { return del(m).leaveWorkspace() }
		if nm, cmd, asked := m.askBeforeDrop(gone, leave); asked {
			return nm, cmd
		}
		return leave(m)
	}
	return del(m), nil
}

// applyMove moves member path by d places.
func (p *workspacePopup) applyMove(m Model, e workspace.Entry, path string, d int) (Model, tea.Cmd) {
	m = p.saveEntry(m, e.Name, func(fresh *workspace.Entry) {
		if i := fresh.IndexOf(path); i >= 0 {
			fresh.Move(i, d)
		}
	}, e.Name)
	e = p.current(e)
	p.msel = max(0, e.IndexOf(path))
	return m.applyWorkspaceEdit(e)
}

// applySettings makes path the settings repository, or clears it when it is.
func (p *workspacePopup) applySettings(m Model, e workspace.Entry, path string) (Model, tea.Cmd) {
	m = p.saveEntry(m, e.Name, func(fresh *workspace.Entry) {
		if fresh.SettingsRepo != "" && model.KeyOf(fresh.SettingsRepo) == model.KeyOf(path) {
			fresh.SettingsRepo = ""
		} else if fresh.Has(path) {
			fresh.SettingsRepo = path
		}
	}, e.Name)
	return m.applyWorkspaceEdit(p.current(e))
}

// workspaceEditApply runs a request from the page. A non-empty
// reply.DropSessions means it stopped to ask: nothing happened.
func (m Model) workspaceEditApply(req domain.WorkspaceRequest) (Model, tea.Cmd, domain.WorkspaceReply, error) {
	drop := &actorDropState{drop: req.Drop}
	m.actorDrop = drop
	prevStatus := m.statusMsg
	p := &workspacePopup{ws: workspace.Load(m.workspacesPath()), cur: -1, exists: map[string]bool{}}
	var cmd tea.Cmd
	var err error
	m, cmd, err = m.runWorkspaceRequest(p, req)
	m.actorDrop = nil
	if err == nil {
		err = p.err
	}
	var reply domain.WorkspaceReply
	if len(drop.asked) > 0 {
		reply.DropSessions = drop.asked
		return m, cmd, reply, err
	}
	if m.statusMsg != prevStatus {
		reply.Status = m.statusMsg
	}
	return m, cmd, reply, err
}

func (m Model) runWorkspaceRequest(p *workspacePopup, req domain.WorkspaceRequest) (Model, tea.Cmd, error) {
	switch req.Kind {
	case "leave":
		nm, cmd := m.leaveWorkspace()
		return nm, cmd, nil
	case "open":
		return m.openWorkspaceNamed(p, req.Name)
	case "launch":
		if m.launchQuestion == nil {
			return m, nil, nil // another tab answered first
		}
		if req.Name == "" {
			m.launchQuestion = nil
			return m, nil, nil
		}
		nm, cmd, err := m.openWorkspaceNamed(p, req.Name)
		if err == nil && (nm.actorDrop == nil || len(nm.actorDrop.asked) == 0) {
			nm.launchQuestion = nil
		}
		return nm, cmd, err
	case "edit":
		return m.runWorkspaceEdit(p, req)
	}
	return m, nil, fmt.Errorf("unknown workspace request %q", req.Kind)
}

func (m Model) openWorkspaceNamed(p *workspacePopup, name string) (Model, tea.Cmd, error) {
	e, ok := workspace.Find(p.ws, name)
	if !ok {
		return m, nil, errors.New(i18n.T("workspace %s was removed elsewhere", name))
	}
	if m.ws != nil && strings.EqualFold(m.ws.name, e.Name) {
		return m, nil, nil
	}
	nm, cmd := m.openWorkspace(e)
	return nm, cmd, nil
}

func (m Model) runWorkspaceEdit(p *workspacePopup, req domain.WorkspaceRequest) (Model, tea.Cmd, error) {
	arg := func(i int) string {
		if i < len(req.Args) {
			return req.Args[i]
		}
		return ""
	}
	if req.Edit == "new" {
		name := strings.TrimSpace(req.Name)
		if err := workspace.ValidName(name); err != nil {
			return m, nil, errors.New(nameRefusal(err))
		}
		nm, taken, _ := p.applyName(m, "", name)
		if taken != "" {
			return nm, nil, errors.New(i18n.T("a workspace named %s exists", taken))
		}
		return nm, nil, nil
	}
	e, ok := workspace.Find(p.ws, req.Name)
	if !ok {
		return m, nil, errors.New(i18n.T("workspace %s was removed elsewhere", req.Name))
	}
	for i := range p.ws {
		if strings.EqualFold(p.ws[i].Name, e.Name) {
			p.cur, p.sel = i, i
		}
	}
	switch req.Edit {
	case "rename":
		name := strings.TrimSpace(arg(0))
		if err := workspace.ValidName(name); err != nil {
			return m, nil, errors.New(nameRefusal(err))
		}
		nm, taken, _ := p.applyName(m, e.Name, name)
		if taken != "" {
			return nm, nil, errors.New(i18n.T("a workspace named %s exists", taken))
		}
		return nm, nil, nil
	case "delete":
		nm, cmd := p.applyDelete(m, e.Name)
		return nm, cmd, nil
	case "add":
		p.exists[arg(0)] = true
		nm, cmd := m.addResolved(p, wsPathResolvedMsg{main: arg(0), workspace: e.Name})
		return nm, cmd, nil
	case "remove":
		p.msel = e.IndexOf(arg(0))
		if p.msel < 0 {
			return m, nil, nil
		}
		nm, cmd := m.removeMember(p)
		return nm, cmd, nil
	case "move":
		d, err := strconv.Atoi(arg(1))
		if err != nil {
			return m, nil, fmt.Errorf("bad move %q", arg(1))
		}
		nm, cmd := p.applyMove(m, e, arg(0), d)
		return nm, cmd, nil
	case "settings":
		nm, cmd := p.applySettings(m, e, arg(0))
		return nm, cmd, nil
	}
	return m, nil, fmt.Errorf("unknown workspace edit %q", req.Edit)
}
