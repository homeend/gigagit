package web

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
	stop       func() // ends its watcher or poll; nil = not watched
	watcher    bool   // a real watcher: its own events report a fetch's refs
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
	containing := workspace.Containing(entries, model.KeyOf(main))
	if len(containing) == 0 {
		return nil
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
	return nil
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
		e, ok := o.question[strings.ToLower(req.Name)]
		pending := o.question != nil
		o.mu.Unlock()
		if !pending {
			return domain.WorkspaceReply{}, nil // another tab answered first
		}
		o.endQuestion()
		if req.Name == "" || !ok {
			return domain.WorkspaceReply{}, nil
		}
		return o.open(ctx, e, "keep")
	case "edit":
		return o.edit(ctx, req)
	}
	return domain.WorkspaceReply{}, fmt.Errorf("unknown workspace request %q", req.Kind)
}

func goneElsewhere(name string) error { return fmt.Errorf("workspace %s was removed elsewhere", name) }

// askDrop is askBeforeDrop's rule: running sessions in members about to go ask
// first (""), keep goes on, stop kills them and goes on. asked = nothing happened.
func (o *workspaceOwner) askDrop(ctx context.Context, gone []*ownedMember, drop string) (domain.WorkspaceReply, bool) {
	var sessions []domain.SessionInfo
	for _, m := range gone {
		if m.svc != nil {
			sessions = append(sessions, o.sessionsIn(ctx, m.svc)...)
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
	if reply, asked := o.askDrop(ctx, gone, drop); asked {
		return reply, nil
	}
	main, err := o.s.service().RepoMainPath(ctx)
	if err != nil {
		return domain.WorkspaceReply{}, err
	}
	if !e.Has(main) {
		target := ""
		for _, p := range e.Repos {
			if statErr(p) == nil {
				target = p
				break
			}
		}
		if target == "" {
			return domain.WorkspaceReply{}, fmt.Errorf("%s: no member exists on this machine", e.Name)
		}
		// Identity first: the page must hear a member switch, not a leave.
		o.install(e, target)
		if err := o.adopt(ctx, target); err != nil {
			o.teardown()
			o.publish()
			return domain.WorkspaceReply{}, err
		}
		return domain.WorkspaceReply{Status: openedLine(e)}, nil
	}
	o.install(e, main)
	return domain.WorkspaceReply{Status: openedLine(e)}, nil
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
// starts the watches and the fetch loop.
func (o *workspaceOwner) install(e workspace.Entry, livePath string) {
	o.mu.Lock()
	byKey := map[model.CheckoutKey]*ownedMember{}
	for _, m := range o.members {
		byKey[model.KeyOf(m.path)] = m
	}
	members := make([]*ownedMember, 0, len(e.Repos))
	live := 0
	for i, p := range e.Repos {
		key := model.KeyOf(p)
		m, ok := byKey[key]
		if ok {
			delete(byKey, key)
		} else {
			m = &ownedMember{path: p, svc: o.s.open(p)}
		}
		m.missing = statErr(p) != nil
		if m.missing {
			m.svc = nil
		} else if m.svc == nil {
			m.svc = o.s.open(p)
		}
		members = append(members, m)
		if model.SamePath(p, livePath) {
			live = i
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
	m.stop, m.watcher = nil, false
}

// rewatch watches every sleeping member and not the one on screen, whose
// changes the live hub already reports.
func (o *workspaceOwner) rewatch() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i, m := range o.members {
		if i == o.live || m.missing || m.svc == nil {
			stopWatch(m)
			continue
		}
		if m.stop == nil {
			o.startWatch(m)
		}
	}
}

// startWatch runs m's watcher, else a poll: every event tells the page. Called
// with o.mu held; the read of the common dir is stat-cheap and cached.
func (o *workspaceOwner) startWatch(m *ownedMember) {
	ctx, cancel := context.WithCancel(context.Background())
	done := func() { cancel() }
	path, s := m.path, o.s
	if common, err := m.svc.GitCommonDir(ctx); err == nil && common != "" && gitwatch.Supported(common) {
		if w, werr := gitwatch.New(gitwatch.Plan(common, common, []gitwatch.Source{gitwatch.Branches, gitwatch.Worktrees}), ownerWatchDebounce); werr == nil {
			m.watcher = true
			done = func() {
				cancel()
				_ = w.Close()
			}
			go func() {
				for range w.Events() {
					s.MemberChanged(path)
				}
			}()
			m.stop = done
			return
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
	m.stop = done
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
			o.fetchOnce(ctx, now)
		}
	}
}

// fetchOnce runs at most one sleeping member's due fetch, so they go one after
// another (the terminal's memberFetchesDue). A member seen for the first time is
// stamped: no burst when a workspace opens.
func (o *workspaceOwner) fetchOnce(ctx context.Context, now time.Time) {
	o.mu.Lock()
	var due *ownedMember
	for i, m := range o.members {
		if i == o.live || m.missing || m.svc == nil {
			continue
		}
		secs := memberFetchSeconds(ctx, o.s, m.svc)
		if secs <= 0 {
			continue
		}
		last, seen := o.lastRun[m.path]
		if !seen {
			o.lastRun[m.path] = now
			continue
		}
		if due == nil && now.Sub(last) >= time.Duration(secs)*time.Second {
			due = m
		}
	}
	if due != nil {
		o.lastRun[due.path] = now
	}
	var svc *domain.Service
	var path string
	var watcher bool
	if due != nil {
		svc, path, watcher = due.svc, due.path, due.watcher
	}
	o.mu.Unlock()
	if svc == nil {
		return
	}
	if err := o.fetch(ctx, svc); err == nil && !watcher {
		o.s.MemberChanged(path) // no watcher saw the remote-tracking refs move
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
	o.members[at].lastViewed = svc.Root()
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
		} else if next == "" && !x.missing && x.svc != nil {
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
	main, err := o.s.service().RepoMainPath(ctx)
	if err != nil {
		return err
	}
	o.install(e, main)
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
