package web

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/gitwatch"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

// workspaceOwner is what is open under standalone gg web, where no terminal
// holds it (spec 2026-10-31-web-workspaces §Standalone owner). It fills the
// seams the terminal fills under gg --web, so the page cannot tell them apart.

const (
	ownerPollInterval  = 30 * time.Second // an unwatchable member, as the terminal's
	ownerWatchDebounce = 500 * time.Millisecond
	ownerFetchTick     = 5 * time.Second
	ownerFetchMinimum  = 10 // seconds: [refresh] min_seconds' default
)

type ownedMember struct {
	path       string
	svc        *domain.Service
	missing    bool
	lastViewed string
	stop       func()      // ends its watcher or poll; nil = not watched
	dir        os.FileInfo // the directory the watch was set on: deleted and recreated, it is dead
}

type workspaceOwner struct {
	s *Server

	// Seams: the fetch, the sessions of a member, and stopping one.
	fetch      func(ctx context.Context, svc *domain.Service) error
	sessionsIn func(ctx context.Context, svc *domain.Service) []domain.SessionInfo
	kill       func(id domain.SessionID) error

	actMu sync.Mutex // one request at a time, adopt included

	mu       sync.Mutex
	name     string
	settings string
	members  []*ownedMember
	live     int
	openedIn string
	question map[string]workspace.Entry // the launch question's workspaces; nil = none
	lastRun  map[string]time.Time       // member path → its last fetch

	cancel context.CancelFunc // ends the fetch loop
}

func newWorkspaceOwner(s *Server) *workspaceOwner {
	o := &workspaceOwner{
		s:          s,
		fetch:      domainFetch,
		sessionsIn: domainSessionsIn,
		kill:       func(id domain.SessionID) error { return domain.Sessions().Kill(id) },
		lastRun:    map[string]time.Time{},
	}
	s.SetWorkspaceActor(o.act)
	s.mu.Lock()
	s.ws.owner = o
	s.mu.Unlock()
	return o
}

func domainSessionsIn(ctx context.Context, svc *domain.Service) []domain.SessionInfo {
	if len(domain.Sessions().List()) == 0 {
		return nil // no git call on a member's behalf when nothing runs anywhere
	}
	wts, err := svc.Worktrees(ctx)
	if err != nil {
		return nil
	}
	return domain.RunningSessionsIn(wts)
}

// start opens the workspace --workspace named, or holds the launch question for
// a member of any. An unknown name fails here, before a port is bound.
func (o *workspaceOwner) start(ctx context.Context, name string) error {
	svc := o.s.service()
	main, err := svc.RepoMainPath(ctx)
	if err != nil {
		return err
	}
	entries := workspace.NormalizeEntries(workspace.Load(o.s.workspacesPath()))
	if name != "" {
		if err := workspace.Check(o.s.workspacesPath()); err != nil {
			return err
		}
		e, ok := workspace.Find(entries, name)
		if !ok {
			return fmt.Errorf("no workspace named %q", name)
		}
		if !e.Has(main) {
			return fmt.Errorf("%s: this repository is not a member", name)
		}
		o.actMu.Lock()
		defer o.actMu.Unlock()
		_, err := o.open(ctx, e, "keep")
		return err
	}
	o.askLaunch(main, entries)
	return nil
}

// askLaunch holds the launch question for the workspaces holding main; none ends it.
func (o *workspaceOwner) askLaunch(main string, entries []workspace.Entry) {
	containing := workspace.Containing(entries, model.KeyOf(main))
	if len(containing) == 0 {
		o.endQuestion()
		return
	}
	q := &domain.WorkspaceQuestion{Repo: filepath.Base(main)}
	held := map[string]workspace.Entry{}
	for _, e := range containing {
		names := make([]string, 0, len(e.Repos))
		for _, r := range e.Repos {
			names = append(names, filepath.Base(r))
		}
		q.Rows = append(q.Rows, domain.WorkspaceQuestionRow{Name: e.Name, Label: "Open workspace " + e.Name + " — " + strings.Join(names, ", ")})
		held[strings.ToLower(e.Name)] = e
	}
	o.mu.Lock()
	o.question = held
	o.mu.Unlock()
	o.s.SetWorkspaceQuestion(q)
}

// refreshQuestion re-asks a pending launch question after the registry changed.
func (o *workspaceOwner) refreshQuestion(ctx context.Context) {
	o.mu.Lock()
	pending := o.question != nil
	o.mu.Unlock()
	if !pending {
		return
	}
	if main, err := o.s.service().RepoMainPath(ctx); err == nil {
		o.askLaunch(main, workspace.NormalizeEntries(workspace.Load(o.s.workspacesPath())))
	}
}

// endQuestion ends the launch question, whichever way.
func (o *workspaceOwner) endQuestion() {
	o.mu.Lock()
	had := o.question != nil
	o.question = nil
	o.mu.Unlock()
	if had {
		o.s.SetWorkspaceQuestion(nil)
	}
}

// act answers a page's request (Server.SetWorkspaceActor).
func (o *workspaceOwner) act(ctx context.Context, req domain.WorkspaceRequest) (domain.WorkspaceReply, error) {
	o.actMu.Lock()
	defer o.actMu.Unlock()
	ctx = context.WithoutCancel(ctx) // a closed tab does not stop a change half way
	switch req.Kind {
	case "leave":
		return o.leave(ctx, req.Drop)
	case "open":
		e, ok := workspace.Find(workspace.NormalizeEntries(workspace.Load(o.s.workspacesPath())), req.Name)
		if !ok {
			return domain.WorkspaceReply{}, goneElsewhere(req.Name)
		}
		return o.open(ctx, e, req.Drop)
	case "launch":
		o.mu.Lock()
		_, ok := o.question[strings.ToLower(req.Name)]
		pending := o.question != nil
		o.mu.Unlock()
		if !pending {
			return domain.WorkspaceReply{}, nil // another tab answered first
		}
		o.endQuestion()
		if req.Name == "" || !ok {
			return domain.WorkspaceReply{}, nil
		}
		e, ok := workspace.Find(workspace.NormalizeEntries(workspace.Load(o.s.workspacesPath())), req.Name)
		if !ok {
			return domain.WorkspaceReply{}, goneElsewhere(req.Name)
		}
		return o.open(ctx, e, "keep")
	case "edit":
		reply, err := o.edit(ctx, req)
		o.refreshQuestion(ctx)
		return reply, err
	}
	return domain.WorkspaceReply{}, fmt.Errorf("unknown workspace request %q", req.Kind)
}

func goneElsewhere(name string) error { return fmt.Errorf("workspace %s was removed elsewhere", name) }

// askDrop is askBeforeDrop's rule: running sessions in members about to go ask
// first (""), keep goes on, stop kills them and goes on. asked = nothing happened.
func (o *workspaceOwner) askDrop(ctx context.Context, gone []*ownedMember, drop string) (domain.WorkspaceReply, bool) {
	var sessions []domain.SessionInfo
	for _, m := range gone {
		svc := m.svc
		if svc == nil && !m.missing {
			svc = o.s.open(m.path) // opened a moment ago, still being read
		}
		if svc != nil {
			sessions = append(sessions, o.sessionsIn(ctx, svc)...)
		}
	}
	if len(sessions) == 0 || drop == "keep" {
		return domain.WorkspaceReply{}, false
	}
	if drop == "stop" {
		for _, s := range sessions {
			_ = o.kill(s.ID)
		}
		return domain.WorkspaceReply{}, false
	}
	return domain.WorkspaceReply{DropSessions: sessions}, true
}

// open makes e the open workspace. A repository that is not a member moves to
// e's first existing member first, as the TUI does; members of the workspace
// open now that e lacks are asked about.
func (o *workspaceOwner) open(ctx context.Context, e workspace.Entry, drop string) (domain.WorkspaceReply, error) {
	e = workspace.NormalizeEntry(e)
	o.mu.Lock()
	was := o.name
	var gone []*ownedMember
	for _, m := range o.members {
		if !e.Has(m.path) {
			gone = append(gone, m)
		}
	}
	o.mu.Unlock()
	if was != "" && strings.EqualFold(was, e.Name) {
		return domain.WorkspaceReply{}, nil
	}
	// Nothing moves or stops until it is known the change can happen. The served
	// repository may be gone from disk: that is no member, not a refusal.
	svc := o.s.service()
	main, mainErr := svc.RepoMainPath(ctx)
	if mainErr == nil && e.Has(main) {
		if reply, asked := o.askDrop(ctx, gone, drop); asked {
			return reply, nil
		}
		o.install(e, main, svc.Root())
		return domain.WorkspaceReply{Status: openedLine(e)}, nil
	}
	member, dir, err := o.firstUsable(ctx, e)
	if err != nil {
		return domain.WorkspaceReply{}, err
	}
	if reply, asked := o.askDrop(ctx, gone, drop); asked {
		return reply, nil
	}
	prev, hadPrev := o.currentEntry()
	// Identity first: the page must hear a member switch, not a leave.
	o.install(e, member, "")
	if err := o.adopt(ctx, dir); err != nil {
		if hadPrev && mainErr == nil {
			o.install(prev, main, svc.Root())
		} else {
			o.teardown()
			o.publish()
		}
		return domain.WorkspaceReply{}, err
	}
	return domain.WorkspaceReply{Status: openedLine(e)}, nil
}

// firstUsable is the member an open outside e lands on, and the directory to
// serve it from: the first that opens, its main checkout or, for a bare
// repository, its first worktree.
func (o *workspaceOwner) firstUsable(ctx context.Context, e workspace.Entry) (member, dir string, err error) {
	var first error
	for _, p := range e.Repos {
		if statErr(p) != nil {
			continue
		}
		svc := o.s.open(p)
		d := p
		if wts, werr := svc.Worktrees(ctx); werr == nil {
			if w := domain.DefaultWorktree(wts, ""); w != "" {
				d = w
				svc = o.s.open(d)
			}
		}
		if perr := preflight(ctx, svc, d); perr != nil {
			if first == nil {
				first = perr
			}
			continue
		}
		return p, d, nil
	}
	if first != nil {
		return "", "", first
	}
	return "", "", fmt.Errorf("%s: no member exists on this machine", e.Name)
}

// currentEntry is what is open now, as install takes it.
func (o *workspaceOwner) currentEntry() (workspace.Entry, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e := workspace.Entry{Name: o.name, SettingsRepo: o.settings}
	for _, m := range o.members {
		e.Repos = append(e.Repos, m.path)
	}
	return e, o.name != ""
}

func openedLine(e workspace.Entry) string {
	return fmt.Sprintf("workspace %s: %d repositories", e.Name, len(e.Repos))
}

// adopt swaps the served repository, as a page's re-root does.
func (o *workspaceOwner) adopt(ctx context.Context, path string) error {
	svc := o.s.open(path)
	if err := preflight(ctx, svc, path); err != nil {
		return err
	}
	applyUIPolicies(ctx, svc, o.s.activeRepoConfigPathOr(ctx, svc))
	if err := o.s.adoptServiceBy(ctx, svc); err != nil {
		return err
	}
	o.afterAdopt(ctx, svc)
	return nil
}

// install replaces the members with e's, keeping the ones it already holds, and
// starts the watches and the fetch loop. Nothing here waits on a member's git: a
// new member is read in the background, so a slow one delays neither the port
// nor the page (openMember). liveRoot: the checkout on screen, when it is served.
func (o *workspaceOwner) install(e workspace.Entry, livePath, liveRoot string) {
	missing := make([]bool, len(e.Repos))
	dirs := make([]os.FileInfo, len(e.Repos))
	for i, p := range e.Repos {
		fi, err := os.Stat(p)
		missing[i], dirs[i] = err != nil, fi
	}
	o.mu.Lock()
	byKey := map[model.CheckoutKey]*ownedMember{}
	for _, m := range o.members {
		byKey[model.KeyOf(m.path)] = m
	}
	members := make([]*ownedMember, 0, len(e.Repos))
	live := 0
	var toOpen []*ownedMember
	for i, p := range e.Repos {
		key := model.KeyOf(p)
		m, ok := byKey[key]
		if ok {
			delete(byKey, key)
		} else {
			m = &ownedMember{path: p}
		}
		m.missing = missing[i]
		if m.missing {
			m.svc = nil
		} else if m.svc == nil {
			toOpen = append(toOpen, m)
		}
		if m.dir == nil {
			m.dir = dirs[i]
		}
		members = append(members, m)
		if model.SamePath(p, livePath) {
			live = i
			if liveRoot != "" {
				m.lastViewed = liveRoot
			}
		}
	}
	for _, m := range byKey {
		stopWatch(m)
	}
	if o.name == "" {
		o.openedIn = livePath
	}
	o.name, o.settings, o.members, o.live = e.Name, e.SettingsRepo, members, live
	if o.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		o.cancel = cancel
		go o.fetchLoop(ctx)
	}
	o.mu.Unlock()
	o.endQuestion()
	o.rewatch()
	o.publish()
	for _, m := range toOpen {
		go o.openMember(m)
	}
}

// openMember reads a member's service off every lock, then watches it.
func (o *workspaceOwner) openMember(m *ownedMember) {
	svc := o.s.open(m.path)
	o.mu.Lock()
	held := slices.Contains(o.members, m)
	if held && !m.missing && m.svc == nil {
		m.svc = svc
	}
	o.mu.Unlock()
	if held {
		o.rewatch()
		o.publish()
	}
}

// teardown drops the open workspace.
func (o *workspaceOwner) teardown() {
	o.mu.Lock()
	for _, m := range o.members {
		stopWatch(m)
	}
	o.name, o.settings, o.members, o.live, o.openedIn = "", "", nil, 0, ""
	cancel := o.cancel
	o.cancel = nil
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func stopWatch(m *ownedMember) {
	if m.stop != nil {
		m.stop()
	}
	m.stop = nil
}

// rewatch watches every sleeping member and not the one on screen, whose
// changes the live hub already reports. Starting a watch reads git and walks
// refs, so it happens off the lock.
func (o *workspaceOwner) rewatch() {
	o.mu.Lock()
	var start []*ownedMember
	for i, m := range o.members {
		if i == o.live || m.missing || m.svc == nil {
			stopWatch(m)
			continue
		}
		if m.stop == nil {
			start = append(start, m)
		}
	}
	svcs := make([]*domain.Service, len(start))
	for i, m := range start {
		svcs[i] = m.svc
	}
	o.mu.Unlock()
	for i, m := range start {
		stop := o.startWatch(m.path, svcs[i])
		o.mu.Lock()
		at := slices.Index(o.members, m)
		if at < 0 || at == o.live || m.missing || m.stop != nil {
			stop() // went on screen or away meanwhile, or another rewatch got there first
		} else {
			m.stop = stop
		}
		o.mu.Unlock()
	}
}

// startWatch runs a member's watcher, else a poll: every event tells the page.
func (o *workspaceOwner) startWatch(path string, svc *domain.Service) func() {
	ctx, cancel := context.WithCancel(context.Background())
	s := o.s
	if common, err := svc.GitCommonDir(ctx); err == nil && common != "" && gitwatch.Supported(common) {
		if w, werr := gitwatch.New(gitwatch.Plan(common, common, []gitwatch.Source{gitwatch.Branches, gitwatch.Worktrees}), ownerWatchDebounce); werr == nil {
			go func() {
				for range w.Events() {
					s.MemberChanged(path)
				}
			}()
			return func() {
				cancel()
				_ = w.Close()
			}
		}
	}
	go func() {
		t := time.NewTicker(ownerPollInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.MemberChanged(path)
			}
		}
	}()
	return cancel
}

// recheckMembers notices a member's directory going, coming back, or being
// replaced: stat-cheap, once per tick, because a watch set on the old directory
// never reports any of it. It keeps what the page shows true.
func (o *workspaceOwner) recheckMembers() {
	type look struct {
		m  *ownedMember
		fi os.FileInfo
	}
	o.mu.Lock()
	looks := make([]look, len(o.members))
	for i, m := range o.members {
		looks[i].m = m
	}
	o.mu.Unlock()
	for i := range looks {
		looks[i].fi, _ = os.Stat(looks[i].m.path)
	}
	var moved []string
	var toOpen []*ownedMember
	o.mu.Lock()
	for _, l := range looks {
		m := l.m
		gone := l.fi == nil
		switch {
		case gone && !m.missing:
			m.missing, m.svc = true, nil
			stopWatch(m)
		case !gone && m.missing:
			m.missing, m.dir = false, l.fi
			toOpen = append(toOpen, m)
		case !gone && m.dir != nil && !os.SameFile(m.dir, l.fi):
			m.dir, m.svc = l.fi, nil
			stopWatch(m)
			toOpen = append(toOpen, m)
		case !gone && m.dir == nil:
			m.dir = l.fi
			continue
		default:
			continue
		}
		moved = append(moved, m.path)
	}
	o.mu.Unlock()
	if len(moved) == 0 {
		return
	}
	o.rewatch()
	o.publish()
	for _, p := range moved {
		o.s.MemberChanged(p)
	}
	for _, m := range toOpen {
		o.openMember(m)
	}
}

// watched is the members being watched (tests).
func (o *workspaceOwner) watched() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []string
	for _, m := range o.members {
		if m.stop != nil {
			out = append(out, m.path)
		}
	}
	return out
}

func (o *workspaceOwner) fetchLoop(ctx context.Context) {
	t := time.NewTicker(ownerFetchTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			o.recheckMembers()
			o.fetchOnce(ctx, now)
		}
	}
}

// fetchOnce runs at most one sleeping member's due fetch, so they go one after
// another (the terminal's memberFetchesDue). A member seen for the first time is
// stamped: no burst when a workspace opens.
func (o *workspaceOwner) fetchOnce(ctx context.Context, now time.Time) {
	o.mu.Lock()
	type candidate struct {
		path string
		svc  *domain.Service
	}
	var cands []candidate
	for i, m := range o.members {
		if i != o.live && !m.missing && m.svc != nil {
			cands = append(cands, candidate{m.path, m.svc})
		}
	}
	o.mu.Unlock()
	secs := make([]int, len(cands))
	for i, c := range cands {
		secs[i] = memberFetchSeconds(ctx, o.s, c.svc) // reads git and config: off the lock
	}
	var due *candidate
	o.mu.Lock()
	for i := range cands {
		if secs[i] <= 0 {
			continue
		}
		last, seen := o.lastRun[cands[i].path]
		if !seen {
			o.lastRun[cands[i].path] = now
			continue
		}
		if due == nil && now.Sub(last) >= time.Duration(secs[i])*time.Second {
			due = &cands[i]
		}
	}
	if due != nil {
		o.lastRun[due.path] = now
	}
	o.mu.Unlock()
	if due == nil {
		return
	}
	// Remote-tracking refs are no watched source: the fetch itself tells the page.
	if err := o.fetch(ctx, due.svc); err == nil {
		o.s.MemberChanged(due.path)
	}
}

// memberFetchSeconds is a member's own [refresh] fetch interval; 0 = off.
func memberFetchSeconds(ctx context.Context, s *Server, svc *domain.Service) int {
	active, err := s.activeRepoConfigPath(ctx, svc)
	if err != nil {
		return 0
	}
	cfg, err := config.Load(config.DefaultGlobalPath(), active)
	if err != nil || !cfg.Refresh.Enabled || cfg.Refresh.Fetch <= 0 {
		return 0
	}
	floor := cfg.Refresh.MinSeconds
	if floor <= 0 {
		floor = ownerFetchMinimum
	}
	return max(cfg.Refresh.Fetch, floor)
}

// afterAdoptSerial is afterAdopt for a swap the page asked for directly.
func (o *workspaceOwner) afterAdoptSerial(ctx context.Context, svc *domain.Service) {
	o.actMu.Lock()
	defer o.actMu.Unlock()
	o.afterAdopt(ctx, svc)
}

// afterAdopt follows a swap of the served repository: another member becomes
// the one on screen; a path outside every member leaves, and nothing asks
// (members' sessions keep running), as in the TUI.
func (o *workspaceOwner) afterAdopt(ctx context.Context, svc *domain.Service) {
	o.endQuestionIfMoved(ctx, svc)
	main, err := svc.RepoMainPath(ctx)
	if err != nil {
		return
	}
	o.mu.Lock()
	if o.name == "" {
		o.mu.Unlock()
		return
	}
	at := -1
	for i, m := range o.members {
		if model.SamePath(m.path, main) {
			at = i
		}
	}
	if at < 0 {
		o.mu.Unlock()
		o.teardown()
		o.publish()
		return
	}
	o.live = at
	m := o.members[at]
	m.lastViewed = svc.Root()
	if m.missing || m.svc == nil { // it is served, so it exists: the switch revives it
		m.missing, m.svc = false, svc
		m.dir = nil
	}
	o.mu.Unlock()
	o.rewatch()
	o.publish()
}

// endQuestionIfMoved: the launch question is about the repository it was asked in.
func (o *workspaceOwner) endQuestionIfMoved(ctx context.Context, svc *domain.Service) {
	o.mu.Lock()
	held := o.question
	o.mu.Unlock()
	if held == nil {
		return
	}
	main, err := svc.RepoMainPath(ctx)
	if err != nil {
		return
	}
	for _, e := range held {
		if e.Has(main) {
			return
		}
	}
	o.endQuestion()
}

// publish tells the page what is open now.
func (o *workspaceOwner) publish() {
	o.s.SetWorkspace(o.info())
}

func (o *workspaceOwner) info() *domain.WorkspaceInfo {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.name == "" {
		return nil
	}
	names, paths := make([]string, len(o.members)), make([]string, len(o.members))
	for i, m := range o.members {
		names[i], paths[i] = filepath.Base(m.path), m.path
	}
	labels := workspace.HeadingLabels(names, paths)
	info := &domain.WorkspaceInfo{Name: o.name, Live: o.live}
	for i, m := range o.members {
		info.Members = append(info.Members, domain.WorkspaceMember{Path: m.path, Label: labels[i], Missing: m.missing, LastViewed: m.lastViewed, Svc: m.svc})
	}
	// The effective one: settings_repo, else the member opened in, else the live.
	for _, candidate := range []string{o.settings, o.openedIn} {
		for _, m := range o.members {
			if candidate != "" && !m.missing && model.SamePath(m.path, candidate) && info.SettingsRepo == "" {
				info.SettingsRepo = m.path
			}
		}
	}
	if info.SettingsRepo == "" && o.live < len(o.members) {
		info.SettingsRepo = o.members[o.live].path
	}
	return info
}

// leave keeps the member on screen as the single repository.
func (o *workspaceOwner) leave(ctx context.Context, drop string) (domain.WorkspaceReply, error) {
	o.mu.Lock()
	name := o.name
	var gone []*ownedMember
	for i, m := range o.members {
		if i != o.live {
			gone = append(gone, m)
		}
	}
	o.mu.Unlock()
	if name == "" {
		return domain.WorkspaceReply{}, nil
	}
	if reply, asked := o.askDrop(ctx, gone, drop); asked {
		return reply, nil
	}
	o.teardown()
	o.publish()
	return domain.WorkspaceReply{Status: "left workspace " + name}, nil
}

func (o *workspaceOwner) isOpen(name string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.name != "" && strings.EqualFold(o.name, name)
}

// edit runs one change to workspaces.toml. The order is the TUI's: ask, switch
// away if needed, then save, so a refused switch leaves the file as it was. The
// registry is re-read under its lock, so another gg's edit survives.
func (o *workspaceOwner) edit(ctx context.Context, req domain.WorkspaceRequest) (domain.WorkspaceReply, error) {
	path := o.s.workspacesPath()
	arg := func(i int) string {
		if i < len(req.Args) {
			return req.Args[i]
		}
		return ""
	}
	none := domain.WorkspaceReply{}
	switch req.Edit {
	case "new":
		name := strings.TrimSpace(req.Name)
		if err := workspace.ValidName(name); err != nil {
			return none, err
		}
		var taken string
		_, err := workspace.Update(path, func(ws []workspace.Entry) []workspace.Entry {
			if other, ok := workspace.Find(ws, name); ok {
				taken = other.Name
				return ws
			}
			return workspace.Upsert(ws, workspace.Entry{Name: name})
		})
		if err != nil {
			return none, err
		}
		if taken != "" {
			return none, fmt.Errorf("a workspace named %s exists", taken)
		}
		return none, nil
	case "rename":
		name := strings.TrimSpace(arg(0))
		if err := workspace.ValidName(name); err != nil {
			return none, err
		}
		var taken string
		gone := false
		_, err := workspace.Update(path, func(ws []workspace.Entry) []workspace.Entry {
			if other, ok := workspace.Find(ws, name); ok && !strings.EqualFold(other.Name, req.Name) {
				taken = other.Name
				return ws
			}
			if _, ok := workspace.Find(ws, req.Name); !ok {
				gone = true
				return ws
			}
			return workspace.Rename(ws, req.Name, name)
		})
		switch {
		case err != nil:
			return none, err
		case gone:
			return none, goneElsewhere(req.Name)
		case taken != "":
			return none, fmt.Errorf("a workspace named %s exists", taken)
		}
		if o.isOpen(req.Name) {
			o.mu.Lock()
			o.name = name
			o.mu.Unlock()
			o.publish()
		}
		return none, nil
	case "delete":
		if o.isOpen(req.Name) {
			o.mu.Lock()
			var gone []*ownedMember
			for i, m := range o.members {
				if i != o.live {
					gone = append(gone, m)
				}
			}
			o.mu.Unlock()
			if reply, asked := o.askDrop(ctx, gone, req.Drop); asked {
				return reply, nil
			}
		}
		if _, err := workspace.Update(path, func(ws []workspace.Entry) []workspace.Entry { return workspace.Delete(ws, req.Name) }); err != nil {
			return none, err
		}
		if o.isOpen(req.Name) {
			o.teardown()
			o.publish()
			return domain.WorkspaceReply{Status: "left workspace " + req.Name}, nil
		}
		return none, nil
	case "add":
		var addErr error
		ws, err := workspace.Edit(path, req.Name, func(e *workspace.Entry) { addErr = e.Add(arg(0)) })
		if errors.Is(err, workspace.ErrNoSuchWorkspace) {
			return none, goneElsewhere(req.Name)
		}
		switch {
		case err != nil:
			return none, err
		case errors.Is(addErr, workspace.ErrFull):
			return none, fmt.Errorf("a workspace holds at most %d repositories", workspace.MaxRepos)
		case errors.Is(addErr, workspace.ErrDuplicate):
			return none, fmt.Errorf("%s is already a member", filepath.Base(arg(0)))
		}
		return none, o.follow(ctx, ws, req.Name)
	case "remove":
		target := arg(0)
		if o.isOpen(req.Name) {
			if reply, asked, err := o.leaveMember(ctx, target, req.Drop); err != nil || asked {
				return reply, err
			}
		}
		ws, err := workspace.Edit(path, req.Name, func(e *workspace.Entry) { e.Remove(target) })
		if errors.Is(err, workspace.ErrNoSuchWorkspace) {
			return none, goneElsewhere(req.Name)
		}
		if err != nil {
			return none, err
		}
		return none, o.follow(ctx, ws, req.Name)
	case "move":
		d, err := strconv.Atoi(arg(1))
		if err != nil {
			return none, fmt.Errorf("bad move %q", arg(1))
		}
		ws, err := workspace.Edit(path, req.Name, func(e *workspace.Entry) {
			if i := e.IndexOf(arg(0)); i >= 0 {
				e.Move(i, d)
			}
		})
		if errors.Is(err, workspace.ErrNoSuchWorkspace) {
			return none, goneElsewhere(req.Name)
		}
		if err != nil {
			return none, err
		}
		return none, o.follow(ctx, ws, req.Name)
	case "settings":
		ws, err := workspace.Edit(path, req.Name, func(e *workspace.Entry) {
			if e.SettingsRepo != "" && model.SamePath(e.SettingsRepo, arg(0)) {
				e.SettingsRepo = ""
			} else if e.Has(arg(0)) {
				e.SettingsRepo = arg(0)
			}
		})
		if errors.Is(err, workspace.ErrNoSuchWorkspace) {
			return none, goneElsewhere(req.Name)
		}
		if err != nil {
			return none, err
		}
		return none, o.follow(ctx, ws, req.Name)
	}
	return none, fmt.Errorf("unknown workspace edit %q", req.Edit)
}

// leaveMember prepares removing member target from the open workspace: its
// sessions are asked about, and when it is on screen the page moves to the first
// remaining member first.
func (o *workspaceOwner) leaveMember(ctx context.Context, target, drop string) (domain.WorkspaceReply, bool, error) {
	o.mu.Lock()
	var m *ownedMember
	live := false
	next := ""
	for i, x := range o.members {
		if model.SamePath(x.path, target) {
			m, live = x, i == o.live
		} else if next == "" && !x.missing {
			next = x.path
		}
	}
	o.mu.Unlock()
	if m == nil {
		return domain.WorkspaceReply{}, false, nil
	}
	if reply, asked := o.askDrop(ctx, []*ownedMember{m}, drop); asked {
		return reply, true, nil
	}
	if live && next != "" {
		if err := o.adopt(ctx, next); err != nil {
			return domain.WorkspaceReply{}, false, err
		}
	}
	return domain.WorkspaceReply{}, false, nil
}

// follow makes an open workspace match its saved definition: kept members keep
// their watches, new ones start; the last one gone leaves.
func (o *workspaceOwner) follow(ctx context.Context, ws []workspace.Entry, name string) error {
	if !o.isOpen(name) {
		return nil
	}
	e, ok := workspace.Find(ws, name)
	if !ok {
		return nil
	}
	e = workspace.NormalizeEntry(e)
	if len(e.Repos) == 0 {
		o.teardown()
		o.publish()
		return nil
	}
	svc := o.s.service()
	main, err := svc.RepoMainPath(ctx)
	if err != nil {
		return err
	}
	o.install(e, main, svc.Root())
	return nil
}

// close ends the watches and the fetch loop.
func (o *workspaceOwner) close() {
	o.teardown()
}

func domainFetch(ctx context.Context, svc *domain.Service) error {
	return runFetchQuietly(ctx, svc)
}

// runFetchQuietly is a background `git fetch`: no op slot, no decisions.
func runFetchQuietly(ctx context.Context, svc *domain.Service) error {
	events := make(chan engine.Event, 8)
	go func() {
		for range events {
		}
	}()
	_, err := svc.Execute(ctx, engine.Fetch{}, events, engine.MapDecider{})
	close(events)
	return err
}
