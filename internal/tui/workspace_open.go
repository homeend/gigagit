package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

// memberLoadedMsg is a member's first (or refreshed) read: the two lists
// its group shows and its config overlay, addressed by its service.
type memberLoadedMsg struct {
	svc      *domain.Service
	branches []model.Branch
	wts      worktreesPayload
	cfg      config.Config // the member's own overlay (global + its .gg.toml); MergeMember'd on arrival
	err      error
}

// workspacesPath is the registry file beside the repositories registry.
func (m Model) workspacesPath() string { return domain.WorkspacesPath(m.statePath) }

// memberBySvc is the member whose service is svc (a load's address).
func (m Model) memberBySvc(svc *domain.Service) *repoView {
	if m.ws == nil || svc == nil {
		return nil
	}
	for _, r := range m.ws.members {
		if r.svc == svc {
			return r
		}
	}
	return nil
}

// resolveStartWorkspace is `--workspace <name>` checked up front: the
// workspace must exist and the opened repository must be a member, else an
// error and nothing opens.
func resolveStartWorkspace(svc *domain.Service, path, name string) (workspace.Entry, error) {
	e, ok := workspace.Find(workspace.Load(path), name)
	if !ok {
		return workspace.Entry{}, fmt.Errorf("no workspace named %q", name)
	}
	main, err := svc.RepoMainPath(context.Background())
	if err != nil {
		return workspace.Entry{}, err
	}
	if !e.Has(main) {
		return workspace.Entry{}, fmt.Errorf("%s: this repository is not a member", name)
	}
	return e, nil
}

// startWorkspaceIfAsked opens the workspace `--workspace` named (or an
// open asked before a re-root), once the first health probe has landed:
// the repository slot exists and the worktree list names its main
// checkout.
func (m Model) startWorkspaceIfAsked() (Model, tea.Cmd) {
	if m.startWorkspace == nil || m.ws != nil {
		return m, nil
	}
	if home := m.homeRepo(); home == nil || m.home == "" || !home.mainKnown {
		return m, nil // the probe beat the load (a re-root): the request waits for the seed — the Update tail retries
	}
	e := *m.startWorkspace
	m.startWorkspace = nil
	return m.openWorkspace(e)
}

// memberOfThisRepo reports whether e names this repository: its main
// checkout by identity, or — while the home identity is still the launch
// path — any of its listed worktrees. The second form also corrects the
// identity to the main checkout.
func (m Model) memberOfThisRepo(e workspace.Entry, home *repoView) (Model, bool) {
	if e.Has(home.mainPath) {
		return m, true
	}
	for _, p := range e.Repos {
		if m.isRepoWorktree(p) {
			m = m.setHomeRepoMain(m.mainWorktreePath(p))
			return m, true
		}
	}
	return m, false
}

// openWorkspace opens e with the viewed repository as its live member.
// A repository that is not a member re-roots into e's first existing
// member first (today's repository-switch rules) and opens once that
// load has landed. The other members get a slot each, a sharing service
// rooted at their main checkout, and a background first read
// (memberLoadCmd) — never m.loading; a path that does not exist is kept
// as a missing member with no service (spec §1).
func (m Model) openWorkspace(e workspace.Entry) (Model, tea.Cmd) {
	home := m.liveRepo()
	if home == nil || m.home == "" {
		m.statusMsg = i18n.T("the repository is still loading — switch once it is shown")
		return m, nil
	}
	var member bool
	if m, member = m.memberOfThisRepo(e, home); !member {
		for _, p := range e.Repos {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				m.startWorkspace = &e
				tm, cmd := m.guardedReRoot(p, false, true)
				nm := tm.(Model)
				if nm.switchTarget != p {
					nm.startWorkspace = nil // refused (unreachable): the refusal is on the status line, nothing to retry
				}
				return nm, cmd
			}
		}
		m.statusMsg = i18n.T("workspace %s: none of its repositories exists here", e.Name)
		return m, nil
	}
	if m.ws != nil {
		// Another workspace is open: reconcile — members both hold keep
		// their slots and windows; the rest go (asked about first when
		// they run sessions), new ones load.
		gone := m.membersNotIn(e)
		if nm, cmd, asked := m.askBeforeDrop(gone, func(m Model) (Model, tea.Cmd) { return m.openWorkspace(e) }); asked {
			return nm, cmd
		}
		m.ws.name, m.ws.settings = e.Name, e.SettingsRepo
		nm, cmd := m.reopenWorkspace(e)
		nm.statusMsg = i18n.T("workspace %s: %d repositories", e.Name, len(e.Repos))
		return nm, cmd
	}
	members := make([]*repoView, 0, len(e.Repos))
	var cmds []tea.Cmd
	live := 0
	for _, p := range e.Repos {
		if model.KeyOf(p) == home.key || m.isRepoWorktree(p) {
			live = len(members)
			members = append(members, home)
			continue
		}
		r := newRepoView(p, nil)
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			r.missing = true
			members = append(members, r)
			continue
		}
		r.svc = domain.OpenTUISharing(p, home.svc)
		r.feed = r.svc.CommitFeed()
		members = append(members, r)
		cmds = append(cmds, memberLoadCmd(r))
	}
	m.ws = &workspaceState{name: e.Name, members: members, live: live, settings: e.SettingsRepo}
	m.homeRepoView = home
	var cfgCmd tea.Cmd
	m, cfgCmd = m.applyWorkspaceConfig()
	m.statusMsg = i18n.T("workspace %s: %d repositories", e.Name, len(members))
	return m, tea.Batch(append(cmds, cfgCmd)...)
}

// memberLoadCmd is a member's read: its branches, its worktree list (with
// the head times and marks the Worktrees rows need) and its config
// overlay, off-thread, addressed by its service.
func memberLoadCmd(r *repoView) tea.Cmd {
	svc, main := r.svc, r.mainPath
	return func() tea.Msg {
		ctx := context.Background()
		msg := memberLoadedMsg{svc: svc}
		bs, err := svc.Branches(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.branches = bs
		wts, err := svc.Worktrees(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		shas := make([]string, 0, len(wts))
		for _, w := range wts {
			if w.Head != "" {
				shas = append(shas, w.Head)
			}
		}
		times, _ := svc.CommitTimes(ctx, shas)
		if times == nil {
			times = map[string]int64{}
		}
		msg.wts = worktreesPayload{worktrees: wts, headTimes: times, marks: worktreeMarksFor(svc, wts)}
		cfg, cerr := config.Load(config.DefaultGlobalPath(), memberConfigPath(main))
		if cerr != nil {
			cfg = config.Defaults()
		}
		msg.cfg = cfg
		return msg
	}
}

// applyMemberLoad lands a member's read in its slot (a sleeping member's
// group shows it at once) or, for the live member, in the live lists.
// Head times and marks are keyed by sha / path and merge into the shared
// maps. A failed read leaves the member unloaded; its heading stays.
func (m Model) applyMemberLoad(msg memberLoadedMsg) Model {
	r := m.memberBySvc(msg.svc)
	if r == nil {
		return m
	}
	if msg.err != nil {
		m.statusMsg = i18n.T("%s: %s", r.name, msg.err.Error())
		return m
	}
	r.cfg = config.MergeMember(m.wsCfg, msg.cfg)
	applyServicePolicies(r.svc, r.cfg)
	if r == m.liveRepo() {
		// The member became live before its first load landed: the lists
		// fill the live panels (dropping them would leave them empty).
		m.cfg = r.cfg
		if !r.loaded {
			m.branches, m.worktrees = msg.branches, msg.wts.worktrees
			m.bfMemo.invalidate()
			m = m.markHead(m.worktreeBranch(m.currentWorktree)).rebuildCommitGraph()
		}
		r.loaded = true
	} else {
		r.branches, r.worktrees = msg.branches, msg.wts.worktrees
		r.loaded = true
		m.memberWatchKick = true // a sleeping member, loaded: its watcher starts (the Update tail)
	}
	times := make(map[string]int64, len(m.headTimes)+len(msg.wts.headTimes))
	for k, v := range m.headTimes {
		times[k] = v
	}
	for k, v := range msg.wts.headTimes {
		times[k] = v
	}
	m.headTimes = times
	if len(msg.wts.marks) > 0 {
		marks := make(map[string]domain.WorktreeMark, len(m.worktreeMarks)+len(msg.wts.marks))
		for k, v := range m.worktreeMarks {
			marks[k] = v
		}
		for k, v := range msg.wts.marks {
			marks[k] = v
		}
		m.worktreeMarks = marks
	}
	return m
}

// memberConfigPath is a member's ONE active per-repo config file: its
// machine-private file when that exists, else the committed .gg.toml —
// the rule the load path uses (load.go / source.go), so a theme or a
// template kept private is not lost when a workspace opens.
func memberConfigPath(main string) string {
	return config.ActiveRepoConfigPath(filepath.Join(main, ".gg.toml"), config.PrivateRepoPath(main))
}

// settingsRepo is the member whose process-wide config the workspace
// uses: the one `settings_repo` names, else the member gg was opened in.
func (m Model) settingsRepo() *repoView {
	if m.ws == nil {
		return m.homeRepoView
	}
	if m.ws.settings != "" {
		key := model.KeyOf(m.ws.settings)
		for _, r := range m.ws.members {
			if r.key == key && !r.missing {
				return r
			}
		}
	}
	if r := m.homeRepo(); r != nil {
		return r
	}
	return m.liveRepo()
}

// applyWorkspaceConfig makes the settings repository's config the
// process-wide source (m.wsCfg) and re-merges every member's own sections
// over it; the theme and language follow at once. Called when a workspace
// opens and when its settings repository changes.
func (m Model) applyWorkspaceConfig() (Model, tea.Cmd) {
	sr := m.settingsRepo()
	if sr == nil {
		return m, nil
	}
	cfg, err := config.Load(config.DefaultGlobalPath(), memberConfigPath(sr.mainPath))
	if err != nil {
		cfg = config.Defaults()
	}
	m.wsCfg = cfg
	m.repoConfigPath = memberConfigPath(sr.mainPath) // the Settings popup edits the settings repository's file (its title says so)
	if m.ws != nil {
		for _, r := range m.ws.members {
			if r == m.liveRepo() {
				continue
			}
			if r.loaded {
				r.cfg = config.MergeMember(m.wsCfg, r.cfg)
				if r.svc != nil {
					applyServicePolicies(r.svc, r.cfg)
				}
			}
		}
	}
	own := m.cfg // the live member's own overlay, re-read from disk as the others' are (memberLoadCmd)
	if live := m.liveRepo(); live != nil {
		if c, err := config.Load(config.DefaultGlobalPath(), memberConfigPath(live.mainPath)); err == nil {
			own = c
		}
	}
	m.cfg = config.MergeMember(m.wsCfg, own)
	if live := m.liveRepo(); live != nil {
		live.cfg = m.cfg
	}
	m = m.applyPoliciesToSlots()
	m = m.applyLanguage()
	return m.applyTheme()
}

// workspaceCfg is a repository's own freshly loaded config as the live
// config should hold it: as is with no workspace, else its per-member
// sections over the workspace's process-wide ones.
func (m Model) workspaceCfg(own config.Config) config.Config {
	if m.ws == nil {
		return own
	}
	return config.MergeMember(m.wsCfg, own)
}

// leaveWorkspace keeps the viewed member as the single repository and
// drops the rest. gg's identity moves to the viewed worktree when home was
// in another member (it cannot stay in a slot that is going).
func (m Model) leaveWorkspace() (Model, tea.Cmd) {
	if m.ws == nil {
		return m, nil
	}
	name := m.ws.name
	live := m.liveRepo()
	var gone []*repoView
	for _, r := range m.ws.members {
		if r != live {
			gone = append(gone, r)
		}
	}
	if nm, cmd, asked := m.askBeforeDrop(gone, Model.leaveWorkspace); asked {
		return nm, cmd
	}
	var cmd tea.Cmd
	if m.homeRepo() != live {
		m, cmd = m.adoptView()
	}
	m = m.closeMembersExcept(live)
	m.homeRepoView = live
	m.ws = nil
	cfg, err := config.Load(config.DefaultGlobalPath(), memberConfigPath(live.mainPath))
	m.repoConfigPath = memberConfigPath(live.mainPath)
	if err == nil {
		m.cfg = cfg
		m = m.applyPoliciesToSlots()
		m = m.applyLanguage()
		var themeCmd tea.Cmd
		m, themeCmd = m.applyTheme()
		cmd = tea.Batch(cmd, themeCmd)
	}
	m.statusMsg = i18n.T("left workspace %s", name)
	return m, cmd
}

// membersNotIn is the open workspace's members that e does not hold.
func (m Model) membersNotIn(e workspace.Entry) []*repoView {
	var out []*repoView
	if m.ws == nil {
		return nil
	}
	for _, r := range m.ws.members {
		if !e.Has(r.mainPath) {
			out = append(out, r)
		}
	}
	return out
}

// askBeforeDrop pushes the drop prompt when any of the members about to go
// has running sessions (spec §6: leave, remove, open another); then runs
// `then` on the user's answer. asked = the prompt is up, the caller
// returns; false = nothing to ask, the caller proceeds. A prompt already
// answered (the popup's `then` re-enters the caller) is not asked twice:
// the answer marked the members.
func (m Model) askBeforeDrop(gone []*repoView, then func(Model) (Model, tea.Cmd)) (Model, tea.Cmd, bool) {
	var sessions []domain.SessionInfo
	var names []string
	for _, r := range gone {
		if r.dropAnswered {
			continue
		}
		if s := m.runningSessionsIn(r); len(s) > 0 {
			sessions = append(sessions, s...)
			names = append(names, r.name)
		}
	}
	if len(sessions) == 0 {
		return m, nil, false
	}
	for _, r := range gone {
		r.dropAnswered = true // whatever the answer, the question is asked once per drop
	}
	return m.pushLayer(&memberDropPopup{names: names, sessions: sessions, then: func(m Model) (Model, tea.Cmd) {
		nm, cmd := then(m)
		return nm, cmd
	}, cancel: func(m Model) Model {
		for _, r := range gone {
			r.dropAnswered = false
		}
		return m
	}}), nil, true
}

// closeMembersExcept drops every member slot but keep (nil = all): their
// worktree slots, parked windows and queued results go. Member watchers
// are closed here too once they exist (member_watch.go).
func (m Model) closeMembersExcept(keep *repoView) Model {
	if m.ws == nil {
		return m
	}
	for _, r := range m.ws.members {
		if r != keep {
			m = m.dropMember(r)
		}
	}
	return m
}

// dropMember forgets a member's slots. The sessions running in its
// worktrees stay in the process-global manager (the ctrl+\ popup lists
// them); a prompt before dropping one with running sessions is the
// Workspaces popup's (Task 13).
func (m Model) dropMember(r *repoView) Model {
	r.closeWatch()
	r.views = map[model.CheckoutKey]*worktreeView{}
	r.viewed = ""
	r.dropAnswered = false
	return m
}
