package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/workspace"
)

// The Workspaces settings popup (spec §2): define workspaces (a name, up to
// five member repositories in walk order, the settings repository) from the
// command palette. Two levels — the workspace list and one workspace's
// members — plus the add picker and the name box. Every edit saves at once;
// an edit to the OPEN workspace takes effect live (reopenWorkspace).

type wsLevel int

const (
	wsLevelList    wsLevel = iota // the workspaces
	wsLevelMembers                // one workspace's members
	wsLevelPicker                 // add a repository: open now / recents / Path…
	wsLevelInput                  // a name box (new or rename)
	wsLevelConfirm                // delete confirm
)

type workspacePopup struct {
	popupMax
	level wsLevel
	ws    []workspace.Entry
	sel   int // the workspace list cursor
	cur   int // the workspace being edited (members/picker/rename)
	msel  int // the member cursor

	input     textfield
	inputErr  string
	renaming  bool // the name box renames ws[cur] (false: creates)
	psel      int  // picker cursor
	pquery    string
	csel      int // confirm cursor: 0 keep, 1 delete
	resolving bool

	recents []repos.Entry   // the registry, read off-thread (wsProbeCmd)
	exists  map[string]bool // member path → exists, read off-thread; absent = not probed yet
}

// pickKind is a picker row's source.
type pickKind int

const (
	pickOpen   pickKind = iota // a repository open in this gg now
	pickRecent                 // the Repositories registry
	pickPath                   // Path…: a typed path
)

type pickRow struct {
	kind  pickKind
	path  string
	label string
}

// wsPathResolvedMsg is a picked path resolved off-thread to its main
// checkout (the member identity); err = not a git repository.
type wsPathResolvedMsg struct {
	main      string
	err       error
	workspace string // the workspace the pick was for: the result joins it wherever the popup is by then
}

// openWorkspacePopup is the palette's "Workspaces…".
func (m Model) openWorkspacePopup() (Model, tea.Cmd) {
	p := &workspacePopup{ws: workspace.Load(m.workspacesPath()), exists: map[string]bool{}}
	if m.ws != nil {
		for i, e := range p.ws {
			if strings.EqualFold(e.Name, m.ws.name) {
				p.sel = i
			}
		}
	}
	return m.pushLayer(p), p.probeCmd(m)
}

// probeCmd reads what the popup shows but must not read on the Update
// thread: the registry (the picker's recents) and whether each member
// path of every workspace exists.
func (p *workspacePopup) probeCmd(m Model) tea.Cmd {
	statePath := m.statePath
	var paths []string
	for _, e := range p.ws {
		paths = append(paths, e.Repos...)
	}
	return func() tea.Msg {
		msg := wsProbeMsg{recents: repos.Load(statePath), exists: make(map[string]bool, len(paths))}
		for _, path := range paths {
			msg.exists[path] = dirExists(path)
		}
		return msg
	}
}

// applyProbe lands probeCmd's result.
func (p *workspacePopup) applyProbe(msg wsProbeMsg) {
	p.recents = msg.recents
	if p.exists == nil {
		p.exists = map[string]bool{}
	}
	for path, ok := range msg.exists {
		p.exists[path] = ok
	}
}

// missing reports a member path the probe found absent (unprobed = present).
func (p *workspacePopup) missing(path string) bool {
	known, probed := p.exists[path]
	return probed && !known
}

// save applies an edit to the registry AS IT IS NOW — under the file lock,
// re-read, mutated, written — so an edit another gg process made since
// this popup opened survives; the popup then shows the merged list, its
// cursor on the workspace named focus. A failure is said on the status line.
func (p *workspacePopup) save(m Model, mutate func([]workspace.Entry) []workspace.Entry, focus string) Model {
	ws, err := workspace.Update(m.workspacesPath(), mutate)
	return p.saved(m, ws, err, mutate, focus)
}

// saveEntry applies change to the workspace named name as it is on disk
// NOW (another gg's edit of the same entry survives), then shows the
// merged list as save does.
func (p *workspacePopup) saveEntry(m Model, name string, change func(*workspace.Entry), focus string) Model {
	ws, err := workspace.Edit(m.workspacesPath(), name, change)
	return p.saved(m, ws, err, func(ws []workspace.Entry) []workspace.Entry {
		for i := range ws {
			if strings.EqualFold(ws[i].Name, name) {
				change(&ws[i])
				return ws
			}
		}
		e := workspace.Entry{Name: name}
		change(&e)
		return append(ws, e)
	}, focus)
}

// saved lands an Update/Edit result: the merged list, the cursor on
// focus; a failure is said on the status line and the edit applied to the
// popup's own copy so the screen shows what the user did.
func (p *workspacePopup) saved(m Model, ws []workspace.Entry, err error, mutate func([]workspace.Entry) []workspace.Entry, focus string) Model {
	if err != nil {
		m.statusMsg = i18n.T("could not save workspaces: %s", err.Error())
		p.ws = mutate(p.ws)
	} else {
		p.ws = ws
	}
	p.cur, p.sel = -1, 0
	for i, e := range p.ws {
		if strings.EqualFold(e.Name, focus) {
			p.cur, p.sel = i, i
		}
	}
	if p.cur < 0 {
		p.clampSel()
		if p.level == wsLevelMembers || p.level == wsLevelPicker {
			p.level = wsLevelList // the edited workspace is gone (deleted elsewhere)
		}
	}
	return m
}

func (p *workspacePopup) clampSel() {
	if p.sel >= len(p.ws) {
		p.sel = len(p.ws) - 1
	}
	if p.sel < 0 {
		p.sel = 0
	}
	if p.cur >= 0 && p.cur < len(p.ws) {
		if p.msel >= len(p.ws[p.cur].Repos) {
			p.msel = len(p.ws[p.cur].Repos) - 1
		}
	}
	if p.msel < 0 {
		p.msel = 0
	}
}

// pickerRows is the add picker's list: the repositories open now (the
// viewed one and the members of the open workspace), the registry's
// recents not among them, then Path…; the query narrows the first two.
func (p *workspacePopup) pickerRows(m Model) []pickRow {
	var rows []pickRow
	seen := map[model.CheckoutKey]bool{}
	if p.cur >= 0 && p.cur < len(p.ws) {
		for _, r := range p.ws[p.cur].Repos {
			seen[model.KeyOf(r)] = true // a member already: nothing to add
		}
	}
	add := func(kind pickKind, path string) {
		key := model.KeyOf(path)
		if seen[key] {
			return
		}
		seen[key] = true
		rows = append(rows, pickRow{kind: kind, path: path, label: filepath.Base(path) + "  " + path})
	}
	if m.ws != nil {
		for _, r := range m.ws.members {
			add(pickOpen, r.mainPath)
		}
	} else if r := m.homeRepo(); r != nil {
		add(pickOpen, r.mainPath)
	}
	for _, e := range p.recents {
		add(pickRecent, e.Path)
	}
	if p.pquery != "" {
		q := strings.ToLower(p.pquery)
		kept := rows[:0]
		for _, r := range rows {
			if strings.Contains(strings.ToLower(r.label), q) {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	return append(rows, pickRow{kind: pickPath, label: i18n.T("Path…")})
}

// resolveMainCmd resolves a picked path to its main checkout off-thread.
func resolveMainCmd(path, workspaceName string) tea.Cmd {
	return func() tea.Msg {
		svc := domain.OpenTUI(expandHome(path))
		main, err := svc.RepoMainPath(context.Background())
		return wsPathResolvedMsg{main: main, err: err, workspace: workspaceName}
	}
}

// addResolved lands a resolved path in the edited workspace: cap and
// duplicate refusals on the status line (spec §2).
func (m Model) addResolved(p *workspacePopup, msg wsPathResolvedMsg) (Model, tea.Cmd) {
	p.resolving = false
	if msg.err != nil {
		m.statusMsg = i18n.T("not a git repository: %s", msg.err.Error())
		return m, nil
	}
	target := msg.workspace
	if target == "" && p.cur >= 0 && p.cur < len(p.ws) {
		target = p.ws[p.cur].Name
	}
	e, ok := workspace.Find(p.ws, target)
	if !ok {
		return m, nil // the workspace went meanwhile
	}
	switch err := e.Add(msg.main); {
	case errors.Is(err, workspace.ErrFull):
		m.statusMsg = i18n.T("a workspace holds at most %d repositories", workspace.MaxRepos)
		return m, nil
	case errors.Is(err, workspace.ErrDuplicate):
		m.statusMsg = i18n.T("%s is already a member", filepath.Base(msg.main))
		return m, nil
	}
	p.exists[msg.main] = true
	// The pick joins ITS workspace; the screen moves only when the popup
	// still shows that workspace's picker — a user who went elsewhere
	// meanwhile (another workspace, a name box) is not yanked back.
	here := p.level == wsLevelPicker && p.cur >= 0 && p.cur < len(p.ws) && strings.EqualFold(p.ws[p.cur].Name, target)
	focus := target
	if !here && p.cur >= 0 && p.cur < len(p.ws) {
		focus = p.ws[p.cur].Name
	}
	level, input, renaming, msel := p.level, p.input, p.renaming, p.msel
	m = p.saveEntry(m, target, func(fresh *workspace.Entry) { _ = fresh.Add(msg.main) }, focus)
	if here {
		p.level = wsLevelMembers
		e = p.current(e)
		p.msel = max(0, e.IndexOf(msg.main))
	} else {
		p.level, p.input, p.renaming, p.msel = level, input, renaming, msel
		if fresh, ok := workspace.Find(p.ws, target); ok {
			e = fresh
		}
	}
	m.statusMsg = i18n.T("%s added to %s", filepath.Base(msg.main), target)
	return m.applyWorkspaceEdit(e)
}

// current is the popup's copy of e after a save (the merged file's entry
// of that name), else e itself.
func (p *workspacePopup) current(e workspace.Entry) workspace.Entry {
	if p.cur >= 0 && p.cur < len(p.ws) && strings.EqualFold(p.ws[p.cur].Name, e.Name) {
		return p.ws[p.cur]
	}
	return e
}

// applyWorkspaceEdit makes an edit of the OPEN workspace live; an edit of
// another workspace changes only the file.
func (m Model) applyWorkspaceEdit(e workspace.Entry) (Model, tea.Cmd) {
	if m.ws == nil || !strings.EqualFold(m.ws.name, e.Name) {
		return m, nil
	}
	return m.reopenWorkspace(e)
}

// reopenWorkspace reconciles the open workspace with its edited
// definition: members kept keep their slots and watchers, new ones load,
// gone ones are dropped (the viewed one only after the view moved to the
// first remaining member; the last one leaves the workspace), the order
// and the settings repository follow.
func (m Model) reopenWorkspace(e workspace.Entry) (Model, tea.Cmd) {
	e = e.Normalized()
	byKey := map[model.CheckoutKey]*repoView{}
	for _, r := range m.ws.members {
		byKey[r.key] = r
	}
	var cmds []tea.Cmd
	members := make([]*repoView, 0, len(e.Repos))
	for _, path := range e.Repos {
		key := model.KeyOf(path)
		if r, ok := byKey[key]; ok {
			members = append(members, r)
			delete(byKey, key)
			continue
		}
		r := newRepoView(path, domain.OpenTUISharingRooted(path, m.homeSvc())) // its load says whether the path exists (openWorkspace's rule)
		cmds = append(cmds, memberLoadCmd(r))
		members = append(members, r)
	}
	live := m.liveRepo()
	if _, gone := byKey[live.key]; gone {
		if len(members) == 0 {
			nm, cmd := m.leaveWorkspace()
			return nm, cmd
		}
		var target *repoView
		for _, r := range members {
			if !r.missing && r.svc != nil {
				target = r
				break
			}
		}
		if target == nil {
			nm, cmd := m.leaveWorkspace()
			return nm, cmd
		}
		nm, ok := m.switchRepoUnderPopup(target, "")
		if !ok {
			return nm, nil // refused (an operation runs): the edit stays on disk, the slots as they are
		}
		m = nm
		if m.homeRepo() == live {
			var cmd tea.Cmd
			m, cmd = m.adoptView()
			cmds = append(cmds, cmd)
		}
	}
	if hr := m.homeRepo(); hr != nil {
		if _, homeGone := byKey[hr.key]; homeGone && hr != m.liveRepo() {
			// gg's home lives in a member that is going: adopt the viewed
			// worktree first, so home never points at a dropped slot.
			var cmd tea.Cmd
			m, cmd = m.adoptView()
			cmds = append(cmds, cmd)
		}
	}
	for _, r := range byKey {
		var cmd tea.Cmd
		m, cmd = m.dropMember(r)
		cmds = append(cmds, cmd)
	}
	liveNow := m.liveRepo() // read BEFORE the slice is replaced: live is an index into it
	m.ws.members = members
	m.ws.name = e.Name
	m.ws.settings = e.SettingsRepo
	for i, r := range members {
		if r == liveNow {
			m.ws.live = i
		}
	}
	var cfgCmd tea.Cmd
	m, cfgCmd = m.applyWorkspaceConfig()
	m.memberWatchKick = true
	return m, tea.Batch(append(cmds, cfgCmd)...)
}

// switchRepoUnderPopup is switchRepo asked by the Workspaces popup itself:
// the popup steps aside for the swap (a window on top would refuse it and
// a parked one would travel with the leaving worktree) and comes back on
// top of the arriving member.
func (m Model) switchRepoUnderPopup(r *repoView, worktree string) (Model, bool) {
	var top layer
	if p, ok := m.topLayer().(*workspacePopup); ok {
		top = p
		m = m.popLayer()
	}
	nm, ok := m.switchRepo(r, worktree, true)
	if top != nil {
		nm = nm.pushLayer(top)
	}
	return nm, ok
}

// runningSessionsIn is the running sessions in member r's worktrees.
func (m Model) runningSessionsIn(r *repoView) []domain.SessionInfo {
	var out []domain.SessionInfo
	for _, info := range domain.Sessions().List() {
		if info.State != domain.SessionRunning {
			continue
		}
		if mr, ok := m.memberAt(info.Dir); ok && mr == r {
			out = append(out, info)
		}
	}
	return out
}

// removeMember removes p.ws[p.cur]'s member at p.msel: a member of the open
// workspace with running sessions asks first (memberDropPopup), the
// rest removes at once.
func (m Model) removeMember(p *workspacePopup) (Model, tea.Cmd) {
	if p.cur < 0 || p.cur >= len(p.ws) || p.msel < 0 || p.msel >= len(p.ws[p.cur].Repos) {
		return m, nil
	}
	path := p.ws[p.cur].Repos[p.msel]
	if m.ws != nil && strings.EqualFold(m.ws.name, p.ws[p.cur].Name) {
		if r, ok := m.memberAt(path); ok {
			if nm, cmd, asked := m.askBeforeDrop([]*repoView{r}, func(m Model) (Model, tea.Cmd) {
				return m.removeMemberNow(p, path)
			}); asked {
				return nm, cmd
			}
		}
	}
	return m.removeMemberNow(p, path)
}

func (m Model) removeMemberNow(p *workspacePopup, path string) (Model, tea.Cmd) {
	e := p.ws[p.cur]
	e.Remove(path)
	var cmds []tea.Cmd
	if m.ws != nil && strings.EqualFold(m.ws.name, e.Name) {
		if r, ok := m.memberAt(path); ok && r == m.liveRepo() {
			// The member on screen goes: the view moves to the first
			// remaining member FIRST, so a refused switch (an operation
			// runs) refuses the whole edit — file and screen stay in step.
			var target *repoView
			for _, o := range m.ws.members {
				if o != r && !o.missing && o.svc != nil && e.Has(o.mainPath) {
					target = o
					break
				}
			}
			if target != nil {
				nm, ok := m.switchRepoUnderPopup(target, "")
				if !ok {
					return nm, nil
				}
				m = nm
				if m.homeRepo() == r {
					var cmd tea.Cmd
					m, cmd = m.adoptView()
					cmds = append(cmds, cmd)
				}
			}
		}
	}
	m = p.saveEntry(m, e.Name, func(fresh *workspace.Entry) { fresh.Remove(path) }, e.Name)
	e = p.current(e)
	p.msel = max(0, min(len(e.Repos)-1, p.msel))
	nm, cmd := m.applyWorkspaceEdit(e)
	return nm, tea.Batch(append(cmds, cmd)...)
}

func (p *workspacePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch p.level {
	case wsLevelInput:
		return p.updateInput(m, msg)
	case wsLevelConfirm:
		return p.updateConfirm(m, msg)
	case wsLevelPicker:
		return p.updatePicker(m, msg)
	case wsLevelMembers:
		return p.updateMembers(m, msg)
	}
	return p.updateList(m, msg)
}

func (p *workspacePopup) updateList(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	n := len(p.ws)
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
		return m, nil
	case tea.KeyDown:
		if p.sel < n-1 {
			p.sel++
		}
		return m, nil
	case tea.KeyEnter:
		if n == 0 {
			return m, nil
		}
		p.cur, p.msel, p.level = p.sel, 0, wsLevelMembers
		return m, nil
	}
	switch msg.String() {
	case "j":
		if p.sel < n-1 {
			p.sel++
		}
	case "k":
		if p.sel > 0 {
			p.sel--
		}
	case "n":
		p.input, p.inputErr, p.renaming, p.level = newTextField(""), "", false, wsLevelInput
	case "r":
		if n > 0 {
			p.cur = p.sel
			p.input, p.inputErr, p.renaming, p.level = newTextField(p.ws[p.sel].Name), "", true, wsLevelInput
		}
	case "d":
		if n > 0 {
			p.cur, p.csel, p.level = p.sel, 0, wsLevelConfirm
		}
	case "o":
		if n > 0 {
			e := p.ws[p.sel]
			if m.ws != nil && strings.EqualFold(m.ws.name, e.Name) {
				return m.popLayer(), nil
			}
			return m.popLayer().openWorkspace(e)
		}
	}
	return m, nil
}

// nameRefusal words a ValidName error for the box, in the user's language.
func nameRefusal(err error) string {
	switch {
	case errors.Is(err, workspace.ErrNameEmpty):
		return i18n.T("a workspace needs a name")
	case errors.Is(err, workspace.ErrNameLong):
		return i18n.T("a workspace name has at most %d characters", workspace.MaxNameRunes)
	case errors.Is(err, workspace.ErrNameChar):
		return i18n.T("a workspace name may not contain / ? or control characters")
	}
	return err.Error()
}

func (p *workspacePopup) updateInput(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		p.level, p.inputErr = wsLevelList, ""
		return m, nil
	case tea.KeyEnter:
		name := strings.TrimSpace(p.input.Value())
		if err := workspace.ValidName(name); err != nil {
			p.inputErr = nameRefusal(err)
			return m, nil
		}
		old := ""
		if p.renaming {
			old = p.ws[p.cur].Name
		}
		// The duplicate check runs against the file AS IT IS, inside the
		// edit: a workspace another gg created meanwhile is never replaced.
		var taken string
		m = p.save(m, func(ws []workspace.Entry) []workspace.Entry {
			if other, ok := workspace.Find(ws, name); ok && (!p.renaming || !strings.EqualFold(other.Name, old)) {
				taken = other.Name
				return ws
			}
			if p.renaming {
				return workspace.Rename(ws, old, name)
			}
			return workspace.Upsert(ws, workspace.Entry{Name: name})
		}, name)
		if taken != "" {
			p.inputErr = i18n.T("a workspace named %s exists", taken)
			p.level = wsLevelInput
			for i, e := range p.ws {
				if strings.EqualFold(e.Name, old) {
					p.cur, p.sel = i, i
				}
			}
			return m, nil
		}
		if p.renaming && m.ws != nil && strings.EqualFold(m.ws.name, old) {
			m.ws.name = name
		}
		p.level, p.inputErr = wsLevelList, ""
		return m, nil
	default:
		if p.input.HandleEditKey(msg) {
			p.inputErr = ""
		}
	}
	return m, nil
}

func (p *workspacePopup) updateConfirm(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		p.level = wsLevelList
		return m, nil
	case tea.KeyUp, tea.KeyDown:
		p.csel = 1 - p.csel
		return m, nil
	case tea.KeyEnter:
		p.level = wsLevelList
		if p.csel != 1 {
			return m, nil // the safe option: keep
		}
		name := p.ws[p.cur].Name
		del := func(m Model) Model {
			return p.save(m, func(ws []workspace.Entry) []workspace.Entry { return workspace.Delete(ws, name) }, "")
		}
		if m.ws != nil && strings.EqualFold(m.ws.name, name) {
			// The open one: its members' running sessions are asked about
			// BEFORE the definition goes, so Cancel keeps it on disk.
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
	switch msg.String() {
	case "j", "k":
		p.csel = 1 - p.csel
	}
	return m, nil
}

func (p *workspacePopup) updateMembers(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if p.cur < 0 || p.cur >= len(p.ws) {
		p.level = wsLevelList
		return m, nil
	}
	e := p.ws[p.cur]
	n := len(e.Repos)
	switch msg.Type {
	case tea.KeyEsc:
		p.level = wsLevelList
		return m, nil
	case tea.KeyUp:
		if p.msel > 0 {
			p.msel--
		}
		return m, nil
	case tea.KeyDown:
		if p.msel < n-1 {
			p.msel++
		}
		return m, nil
	}
	switch msg.String() {
	case "j":
		if p.msel < n-1 {
			p.msel++
		}
	case "k":
		if p.msel > 0 {
			p.msel--
		}
	case "a":
		p.psel, p.pquery, p.level = 0, "", wsLevelPicker
		return m, p.probeCmd(m)
	case "d":
		return m.removeMember(p)
	case "K", "J":
		if n == 0 {
			return m, nil
		}
		d := -1
		if msg.String() == "J" {
			d = 1
		}
		moved := e.Repos[p.msel]
		m = p.saveEntry(m, e.Name, func(fresh *workspace.Entry) {
			if i := fresh.IndexOf(moved); i >= 0 {
				fresh.Move(i, d)
			}
		}, e.Name)
		e = p.current(e)
		p.msel = max(0, e.IndexOf(moved))
		return m.applyWorkspaceEdit(e)
	case "s":
		if n == 0 {
			return m, nil
		}
		path := e.Repos[p.msel]
		m = p.saveEntry(m, e.Name, func(fresh *workspace.Entry) {
			if fresh.SettingsRepo != "" && model.KeyOf(fresh.SettingsRepo) == model.KeyOf(path) {
				fresh.SettingsRepo = ""
			} else if fresh.Has(path) {
				fresh.SettingsRepo = path
			}
		}, e.Name)
		e = p.current(e)
		return m.applyWorkspaceEdit(e)
	}
	return m, nil
}

func (p *workspacePopup) updatePicker(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	rows := p.pickerRows(m)
	n := len(rows)
	if filterMotion(msg, func(d int) { p.psel = max(0, min(n-1, p.psel+d)) }, popupFilterPage) {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		if p.pquery != "" {
			p.pquery, p.psel = "", 0
			return m, nil
		}
		p.level = wsLevelMembers
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.pquery); len(r) > 0 {
			p.pquery = string(r[:len(r)-1])
		}
		p.psel = 0
		return m, nil
	case tea.KeyEnter:
		if p.psel < 0 || p.psel >= n {
			return m, nil
		}
		row := rows[p.psel]
		if row.kind == pickPath {
			pp := &repoPathPopup{input: newTextField("")}
			target := p.ws[p.cur].Name
			pp.onResolved = func(m Model, top string) (Model, tea.Cmd) {
				p.resolving = true
				return m, resolveMainCmd(top, target)
			}
			return m.pushLayer(pp), nil
		}
		p.resolving = true
		return m, resolveMainCmd(row.path, p.ws[p.cur].Name)
	case tea.KeyRunes:
		p.pquery += string(msg.Runes)
		p.psel = 0
	}
	return m, nil
}

func (p *workspacePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	textW := popupTextWidth(inner)
	var b strings.Builder
	switch p.level {
	case wsLevelInput:
		if p.renaming {
			b.WriteString(i18n.T("Rename workspace %s", p.ws[p.cur].Name) + "\n\n")
		} else {
			b.WriteString(i18n.T("New workspace") + "\n\n")
		}
		b.WriteString(viewField(i18n.T("name: "), p.input, true, popupContentWidth(w)) + "\n")
		if p.inputErr != "" {
			b.WriteString("\n" + st().errorText.Render(p.inputErr) + "\n")
		}
		b.WriteString("\n" + i18n.T("[type] name  [enter] save  [esc] cancel"))
	case wsLevelConfirm:
		b.WriteString(i18n.T("Delete workspace %s? Its repositories stay on disk.", p.ws[p.cur].Name) + "\n\n")
		for i, opt := range []string{i18n.T("Keep it"), i18n.T("Delete the workspace")} {
			b.WriteString(p.row(opt, i == p.csel) + "\n")
		}
		b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] choose  [esc] keep"))
	case wsLevelPicker:
		header := i18n.T("Add a repository to %s", p.ws[p.cur].Name)
		if p.pquery != "" {
			header += "  " + p.pquery + "█"
		}
		b.WriteString(header + "\n\n")
		rows := p.pickerRows(m)
		for i, r := range rows {
			tag := ""
			switch r.kind {
			case pickOpen:
				tag = i18n.T("open now") + "  "
			case pickRecent:
				tag = i18n.T("recent") + "  "
			}
			b.WriteString(p.row(elideNameMiddle(tag+r.label, textW-2), i == p.psel) + "\n")
		}
		b.WriteString("\n" + i18n.T("[enter] add  type to filter  [esc] back"))
	case wsLevelMembers:
		e := p.ws[p.cur]
		b.WriteString(i18n.T("Workspace %s — %d of %d repositories", e.Name, len(e.Repos), workspace.MaxRepos) + "\n\n")
		if len(e.Repos) == 0 {
			b.WriteString(st().dim.Render(i18n.T("  (no repositories yet — a adds one)")) + "\n")
		}
		for i, path := range e.Repos {
			mark := "  "
			if e.SettingsRepo != "" && model.KeyOf(e.SettingsRepo) == model.KeyOf(path) {
				mark = "✎ "
			}
			label := filepath.Base(path) + "  " + elidePath(path, max(8, textW-24))
			if p.missing(path) {
				label = i18n.T("%s (missing)", label)
			}
			b.WriteString(p.row(mark+label, i == p.msel) + "\n")
		}
		b.WriteString("\n" + i18n.T("[a] add  [d] remove  [K/J] move  [s] settings repository  [esc] back"))
	default:
		b.WriteString(i18n.T("Workspaces") + "\n\n")
		if len(p.ws) == 0 {
			b.WriteString(st().dim.Render(i18n.T("  (none yet — n creates one)")) + "\n")
		}
		viewed := ""
		if r := m.liveRepo(); r != nil {
			viewed = r.mainPath
		}
		for i, e := range p.ws {
			mark := "  "
			if (m.ws != nil && strings.EqualFold(m.ws.name, e.Name)) || (viewed != "" && e.Has(viewed)) {
				mark = "● " // the open one, and every workspace the viewed repository belongs to (spec §2)
			}
			names := make([]string, 0, len(e.Repos))
			for _, r := range e.Repos {
				names = append(names, filepath.Base(r))
			}
			b.WriteString(p.row(mark+i18n.T("%s — %d: %s", e.Name, len(e.Repos), strings.Join(names, ", ")), i == p.sel) + "\n")
		}
		b.WriteString("\n" + i18n.T("[n] new  [enter] repositories  [r] rename  [d] delete  [o] open now  [esc] close"))
	}
	box := st().modalStyle.Width(inner).Render(strings.TrimRight(b.String(), "\n")) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// row is one selectable line.
func (p *workspacePopup) row(text string, selected bool) string {
	prefix := "  "
	if selected {
		prefix = "> "
		return st().selectedRow.Render(prefix + text)
	}
	return prefix + text
}

// memberDropPopup asks before a member with running sessions is dropped
// (spec §6): leave them running (hidden, still in the ctrl+\ popup), stop
// them, or cancel. The first option is the safe one.
type memberDropPopup struct {
	popupMax
	names    []string // the members going, with sessions
	sessions []domain.SessionInfo
	then     func(Model) (Model, tea.Cmd)
	cancel   func(Model) Model // esc / Cancel: the drop does not happen
	sel      int
}

func (p *memberDropPopup) options() []string {
	return []string{i18n.T("Leave them running (hidden)"), i18n.T("Stop them"), i18n.T("Cancel")}
}

func (p *memberDropPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	n := len(p.options())
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m = m.popLayer()
		if p.cancel != nil {
			m = p.cancel(m)
		}
		return m, nil
	case tea.KeyUp:
		p.sel = (p.sel - 1 + n) % n
	case tea.KeyDown:
		p.sel = (p.sel + 1) % n
	case tea.KeyEnter:
		sel := p.sel
		m = m.popLayer()
		switch sel {
		case 0:
			return p.then(m)
		case 1:
			for _, s := range p.sessions {
				_ = domain.Sessions().Kill(s.ID)
			}
			return p.then(m)
		}
		if p.cancel != nil {
			m = p.cancel(m)
		}
		return m, nil
	}
	switch msg.String() {
	case "j":
		p.sel = (p.sel + 1) % n
	case "k":
		p.sel = (p.sel - 1 + n) % n
	}
	return m, nil
}

func (p *memberDropPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	textW := popupTextWidth(inner)
	var b strings.Builder
	for _, line := range wrapWidth(i18n.T("%s has %d running sessions — leave them running (hidden), stop them, or cancel?", strings.Join(p.names, ", "), len(p.sessions)), textW, 1<<20) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
	for i, opt := range p.options() {
		prefix := "  "
		row := prefix + opt
		if i == p.sel {
			row = st().selectedRow.Render("> " + opt)
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] choose  [esc] cancel"))
	box := st().modalStyle.Width(inner).Render(strings.TrimRight(b.String(), "\n")) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// dirExists reports an existing directory.
func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// wsProbeMsg is what the Workspaces popup reads off the Update thread: the
// Repositories registry (the picker's recents) and whether each member
// path exists (the "(missing)" marks).
type wsProbeMsg struct {
	recents []repos.Entry
	exists  map[string]bool
}
