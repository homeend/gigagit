package domain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/template"
)

// MaxBriefBytes caps an agent_start prompt (the brief).
const MaxBriefBytes = 256 << 10

// MaxSendBytes caps an agent_send text; MaxNoteBytes an agent_start note.
const (
	MaxSendBytes = 64 << 10
	MaxNoteBytes = 1 << 10
)

type AgentEntry struct {
	ID       string    `json:"id"`
	Parent   string    `json:"parent,omitempty"`
	Tool     string    `json:"tool"`
	Label    string    `json:"label"`
	Worktree string    `json:"worktree"`
	State    string    `json:"state"`
	ExitCode int       `json:"exit_code"`
	Started  time.Time `json:"started"`
	Spawned  bool      `json:"spawned"`
	Mine     bool      `json:"mine"`
	// What the agent is doing, read off its screen: "working" | "idle" |
	// "question" (it waits for a decision — agent_screen has the options);
	// "" for a session gg does not classify. Stalled: nothing printed for
	// two minutes while apparently busy.
	Activity      string    `json:"activity,omitempty"`
	ActivitySince time.Time `json:"activity_since,omitzero"`
	Stalled       bool      `json:"stalled,omitempty"`
}

// AgentScreenResult is agent_screen's answer.
type AgentScreenResult struct {
	State    string // running | exited
	Text     string // the visible screen, trailing blanks trimmed
	Activity string // as AgentEntry.Activity
	Options  []ActivityOption
}

// AgentList is every session of this process; Mine marks caller's descendants.
func AgentList(caller string) []AgentEntry {
	var out []AgentEntry
	for _, in := range Sessions().List() {
		full := FullSessionID(in.ID)
		rec, _ := AgentRecord(full)
		e := AgentEntry{ID: full, Parent: rec.Parent, Tool: in.Label, Label: in.Label,
			Worktree: in.Dir, State: sessionStateName(in.State), ExitCode: in.ExitCode,
			Started: in.Started, Spawned: rec.Spawned, Mine: AgentDescends(full, caller)}
		if a, ok := SessionActivityOf(in.ID); ok {
			e.Activity, e.ActivitySince, e.Stalled = a.Name(), a.Since, a.Stalled
		}
		out = append(out, e)
	}
	return out
}

func sessionOf(full string) (*AgentSession, error) {
	id := localID(full)
	if id == "" {
		return nil, fmt.Errorf("%s is not a session of this gg", full)
	}
	s, ok := Sessions().Get(id)
	if !ok {
		return nil, fmt.Errorf("no session %s", full)
	}
	return s, nil
}

// AgentScreen is target's visible screen as plain text (trailing blanks
// trimmed) with what the agent is doing and, at a question, its choices.
func AgentScreen(target string) (AgentScreenResult, error) {
	s, err := sessionOf(target)
	if err != nil {
		return AgentScreenResult{}, err
	}
	lines := strings.Split(s.Text(), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	res := AgentScreenResult{State: sessionStateName(s.Info().State), Text: strings.TrimRight(strings.Join(lines, "\n"), "\n")}
	if a, ok := SessionActivityOf(s.Info().ID); ok {
		res.Activity, res.Options = a.Name(), a.Options
	}
	return res, nil
}

// reach refuses a target that is not caller's descendant (spec §5).
func reach(caller, target string) error {
	if AgentDescends(target, caller) {
		return nil
	}
	if _, err := sessionOf(target); err != nil {
		return err
	}
	by := "the user"
	if rec, ok := AgentRecord(target); ok && rec.Parent != "" {
		by = rec.Parent
	}
	return fmt.Errorf("%s is not an agent you started (started by %s)", target, by)
}

// AgentSend pastes text (bracketed when the child asked for it), then Enter
// when enter is set, then each key in keys.
func AgentSend(caller, target, text string, enter bool, keys []string) error {
	if err := reach(caller, target); err != nil {
		return err
	}
	if len(text) > MaxSendBytes {
		return fmt.Errorf("the text is larger than %d bytes", MaxSendBytes)
	}
	s, err := sessionOf(target)
	if err != nil {
		return err
	}
	if s.Info().State != SessionRunning {
		return fmt.Errorf("%s has exited", target)
	}
	var evs []ConsoleInput
	for _, k := range keys {
		ck, err := ParseConsoleKeyName(k)
		if err != nil {
			return err
		}
		in, err := ConsoleKeyEvent(ck)
		if err != nil {
			return err
		}
		evs = append(evs, in)
	}
	if text != "" {
		s.Paste(text)
	}
	if enter {
		s.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter})
	}
	for _, in := range evs {
		if in.IsKey {
			s.SendKey(in.Key)
		} else {
			s.SendText(in.Text)
		}
	}
	return nil
}

// AgentKill ends target (and forgets it with remove).
func AgentKill(caller, target string, remove bool) error {
	if err := reach(caller, target); err != nil {
		return err
	}
	id := localID(target)
	if remove {
		return Sessions().KillAndRemove(id)
	}
	return Sessions().Kill(id)
}

// AgentTask is the caller's own brief.
func AgentTask(caller string) (SpawnRecord, error) {
	rec, ok := AgentRecord(caller)
	if !ok || !rec.Spawned {
		return SpawnRecord{}, errors.New("you were not started by an agent: there is no task")
	}
	return rec, nil
}

// ParseConsoleKeyName reads "enter", "esc", "ctrl+c", "shift+tab", "1",
// "space" into the web console's wire key.
func ParseConsoleKeyName(s string) (ConsoleKey, error) {
	orig, mod := s, 0
	for {
		switch {
		case strings.HasPrefix(s, "ctrl+"):
			mod, s = mod|ModCtrl, s[5:]
			continue
		case strings.HasPrefix(s, "alt+"):
			mod, s = mod|ModAlt, s[4:]
			continue
		case strings.HasPrefix(s, "shift+"):
			mod, s = mod|ModShift, s[6:]
			continue
		}
		break
	}
	if s == "space" {
		return ConsoleKey{K: "char", Mod: mod, Text: " "}, nil
	}
	if _, ok := consoleKeyNames[s]; ok {
		return ConsoleKey{K: s, Mod: mod}, nil
	}
	if utf8.RuneCountInString(s) == 1 {
		return ConsoleKey{K: "char", Mod: mod, Text: s}, nil
	}
	return ConsoleKey{}, fmt.Errorf("unknown key %q", orig)
}

var (
	svcCacheMu sync.Mutex
	svcCache   = map[string]*Service{}
)

// ServiceForDir is a Service rooted at dir, cached: the agent channel works
// in the CALLER's repository, whatever the TUI shows now (spec §7 reRoot).
func ServiceForDir(dir string) *Service {
	key := filepath.Clean(dir)
	svcCacheMu.Lock()
	defer svcCacheMu.Unlock()
	if s, ok := svcCache[key]; ok {
		return s
	}
	s := OpenTUI(key) // the TUI hosts every spawn: ssh must never prompt on its raw terminal
	svcCache[key] = s
	return s
}

// cacheService pins dir's Service (tests: one with a test registry dir).
func cacheService(dir string, s *Service) {
	svcCacheMu.Lock()
	svcCache[filepath.Clean(dir)] = s
	svcCacheMu.Unlock()
}

// ResolveWorktreeArg maps a path, a worktree directory name or a branch name
// to a worktree path of this repository.
func (s *Service) ResolveWorktreeArg(ctx context.Context, arg string) (string, error) {
	wts, err := s.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	var hits []string
	for _, w := range wts {
		if SameCheckout(w.Path, arg) {
			return w.Path, nil
		}
		if filepath.Base(w.Path) == arg || w.Branch == arg {
			hits = append(hits, w.Path)
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("no worktree %q in this repository", arg)
	case 1:
		return hits[0], nil
	}
	return "", fmt.Errorf("%q names several worktrees: %s", arg, strings.Join(hits, ", "))
}

type AgentStartRequest struct{ Caller, Worktree, Tool, Prompt, Note string }

type AgentStartResult struct {
	ID       string `json:"id"`
	Worktree string `json:"worktree"`
	Tool     string `json:"tool"`
	Warning  string `json:"warning,omitempty"`
}

// SpawnSpec is what the hosting frontend adds to a request: the console
// size, its child env (GG_INBOX), the channel URL and the approval lookup.
type SpawnSpec struct {
	Req        AgentStartRequest
	Cols, Rows int
	Env        []string
	MCPURL     string
	Approved   func(repoKey, command string) bool
}

// SpawnAgent is agent_start (spec §4.1): checks, claim, the normal session
// start with the kick-off prompt, the record, the claim handover.
func SpawnAgent(ctx context.Context, sp SpawnSpec) (AgentStartResult, *AgentSession, error) {
	req := sp.Req
	callerSess, live := runningLocal(req.Caller)
	if !live {
		return AgentStartResult{}, nil, fmt.Errorf("caller %s is not a running agent of this gg", req.Caller)
	}
	if rec, ok := AgentRecord(req.Caller); ok && rec.Spawned {
		return AgentStartResult{}, nil, errors.New("a spawned agent may not start agents")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return AgentStartResult{}, nil, errors.New("the prompt is empty: pass the worker's task")
	}
	if len(req.Prompt) > MaxBriefBytes {
		return AgentStartResult{}, nil, fmt.Errorf("the prompt is larger than %d bytes", MaxBriefBytes)
	}
	if len(req.Note) > MaxNoteBytes {
		return AgentStartResult{}, nil, fmt.Errorf("the note is larger than %d bytes", MaxNoteBytes)
	}
	svc := ServiceForDir(callerSess.Info().Dir)
	cfg, err := svc.EffectiveConfig(ctx)
	if err != nil {
		return AgentStartResult{}, nil, err
	}
	if len(cfg.Agents.Spawn) == 0 {
		return AgentStartResult{}, nil, errors.New("spawning is off — add the command name to [agents] spawn in the global config")
	}
	var tc *config.ToolCommand
	for _, c := range SessionCommands(cfg, "tui") {
		if strings.EqualFold(c.Name, req.Tool) {
			c := c
			tc = &c
			break
		}
	}
	if tc == nil {
		return AgentStartResult{}, nil, fmt.Errorf("no session command named %q", req.Tool)
	}
	allowed := false
	for _, n := range cfg.Agents.Spawn {
		allowed = allowed || strings.EqualFold(n, tc.Name)
	}
	if !allowed {
		return AgentStartResult{}, nil, fmt.Errorf("%s is not in [agents] spawn", tc.Name)
	}
	if !template.HasPromptSlot(tc.Command) {
		return AgentStartResult{}, nil, fmt.Errorf("%s has no <prompt> slot — accept its template update in Settings → External tools, or add <prompt> to its command", tc.Name)
	}
	repoKey, _ := svc.GitCommonDir(ctx)
	if sp.Approved == nil || !sp.Approved(repoKey, tc.Command) {
		return AgentStartResult{}, nil, fmt.Errorf("approve %s once by starting it from the TUI (Start agent), then retry", tc.Name)
	}
	release, ok := reserveSpawnSlot(cfg.Agents.SpawnCap())
	if !ok {
		return AgentStartResult{}, nil, fmt.Errorf("%d spawned agents are running — the max_spawned cap; wait or kill one", cfg.Agents.SpawnCap())
	}
	started := false
	defer func() { release(started) }()
	path, err := svc.ResolveWorktreeArg(ctx, req.Worktree)
	if err != nil {
		return AgentStartResult{}, nil, err
	}
	held, err := svc.claimHeldBy(ctx, path, req.Caller)
	if err != nil {
		return AgentStartResult{}, nil, err
	}
	created := false
	if !held {
		ac, _, aerr := svc.AgentsConfig(ctx)
		if aerr != nil {
			return AgentStartResult{}, nil, aerr
		}
		if err := svc.ClaimWorktree(ctx, path, req.Caller, req.Note, PolicyFromConfig(ac)); err != nil {
			return AgentStartResult{}, nil, err
		}
		created = true
	}
	sess, _, err := svc.StartAgentSession(ctx, *tc, path, "", sp.Cols, sp.Rows, sp.Env, sp.MCPURL,
		SpawnRecord{Parent: req.Caller, Brief: req.Prompt, Worktree: path, Spawned: true}, AgentKickoff)
	if err != nil {
		if created {
			_, _ = svc.ReleaseWorktree(ctx, path, req.Caller, false)
		}
		return AgentStartResult{}, nil, err
	}
	started = true
	res := AgentStartResult{ID: FullSessionID(sess.Info().ID), Worktree: path, Tool: tc.Name}
	if err := svc.HandOverWorktree(ctx, path, req.Caller, res.ID); err != nil {
		res.Warning = "the worker runs, but its claim stayed with you: " + err.Error()
	}
	return res, sess, nil
}
