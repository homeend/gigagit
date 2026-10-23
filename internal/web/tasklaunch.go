package web

// The AI-task launch dialog (the TUI's task_launch_popup.go): every web AI
// action names an agent × mode × command, then submits to domain.Tasks().
// GET /api/tasks/launch lays the dialog out; POST /api/tasks/submit runs the
// pick (approval first for a command not yet approved).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/template"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/tasks/launch", s.handleTaskLaunch)
		mux.HandleFunc("POST /api/tasks/submit", writeGuard(s.handleTaskSubmit))
	})
}

// Launch modes, the TUI's launchModes order.
const (
	launchBackground = "background" // an agent session, no console opened
	launchForeground = "foreground" // an agent session, its console opens
	launchHeadless   = "headless"   // queued, captured
)

var launchModes = []string{launchBackground, launchForeground, launchHeadless}

// launchLookPath is the "not found" probe (a test seam).
var launchLookPath = exec.LookPath

// launchKinds are the kinds the web launches (conflict comes with part 3).
var launchKinds = map[string]exttool.Category{
	string(exttool.CatCommitMessage):    exttool.CatCommitMessage,
	string(exttool.CatReview):           exttool.CatReview,
	string(exttool.CatConflictComplete): exttool.CatConflictComplete,
}

// launchTarget is what a launch is about: the kind, and for a review the
// target fields the review lane takes.
type launchTarget struct {
	Kind    string   `json:"kind"`
	Target  string   `json:"target"`
	Branch  string   `json:"branch"`
	Sha     string   `json:"sha"`
	Preview string   `json:"preview"`
	Shas    []string `json:"shas"`
}

type launchRowWire struct {
	Mode    string `json:"mode"`
	Name    string `json:"name,omitempty"`
	None    bool   `json:"none,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

type launchChoiceWire struct {
	Agent    string          `json:"agent"`
	Identity string          `json:"identity"`
	Rows     []launchRowWire `json:"rows"`
}

type launchPick struct {
	Identity string `json:"identity"`
	Mode     string `json:"mode"`
	Name     string `json:"name"`
}

// launchPlan is a launch resolved against the repository and the config.
type launchPlan struct {
	kind    exttool.Category
	review  domain.ReviewTarget
	key     string
	choices []domain.TaskChoice
	added   []string
}

// planLaunch resolves t: the kind, the review target, the task key, the
// agents (first run adds the detected agents' interactive rows for
// commit_message and review — the TUI's EnsureInteractiveCommands).
func (s *Server) planLaunch(ctx context.Context, svc *domain.Service, t launchTarget) (launchPlan, int, error) {
	kind, ok := launchKinds[t.Kind]
	if !ok {
		return launchPlan{}, http.StatusBadRequest, fmt.Errorf("no AI task kind %q", t.Kind)
	}
	p := launchPlan{kind: kind}
	if kind == exttool.CatReview {
		target, err := s.reviewTarget(ctx, svc, t.Target, t.Branch, t.Sha, t.Preview, t.Shas)
		if err != nil {
			return launchPlan{}, http.StatusBadRequest, err
		}
		p.review = target
	}
	key, err := svc.TaskKey(ctx, kind, p.review)
	if err != nil {
		return launchPlan{}, http.StatusUnprocessableEntity, err
	}
	p.key = key
	cfg, err := s.effectiveConfig(ctx, svc)
	if err != nil {
		return launchPlan{}, http.StatusInternalServerError, err
	}
	if kind == exttool.CatCommitMessage || kind == exttool.CatReview {
		added, aerr := domain.EnsureInteractiveCommands(cfg, config.DefaultGlobalPath(), s.detections)
		if aerr == nil && len(added) > 0 {
			if cfg, err = s.effectiveConfig(ctx, svc); err != nil {
				return launchPlan{}, http.StatusInternalServerError, err
			}
			p.added = added
		}
	}
	whenOp := ""
	if kind == exttool.CatConflictComplete {
		if st, serr := svc.Status(ctx); serr == nil {
			whenOp = svc.Conflict(ctx, st).Op
		}
	}
	p.choices = launchChoices(cfg, kind, whenOp)
	return p, 0, nil
}

// launchChoices is TaskChoices minus what the dialog cannot run: commands
// asking for <user:…> input, per-file ones, and conflict commands whose
// when_op does not match the paused op (the TUI's launchChoices).
func launchChoices(cfg config.Config, kind exttool.Category, whenOp string) []domain.TaskChoice {
	keep := func(cmds []config.ToolCommand) []config.ToolCommand {
		var out []config.ToolCommand
		for _, tc := range cmds {
			if len(template.UserLabels(tc.Command)) > 0 || tc.PerFile {
				continue
			}
			if tc.WhenOp != "" && tc.WhenOp != whenOp {
				continue
			}
			out = append(out, tc)
		}
		return out
	}
	var out []domain.TaskChoice
	for _, c := range domain.TaskChoices(cfg, kind, "web") {
		c.Headless, c.Interactive = keep(c.Headless), keep(c.Interactive)
		if len(c.Headless)+len(c.Interactive) > 0 {
			out = append(out, c)
		}
	}
	return out
}

// choiceIdentity names an agent in the remembered choice (the TUI's).
func choiceIdentity(c domain.TaskChoice) string {
	if c.AgentID != "" {
		return "id:" + c.AgentID
	}
	return "name:" + c.Agent
}

// commandProgram is a command line's first shell word, unquoted.
func commandProgram(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if q := cmd[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(cmd[1:], q); end >= 0 {
			return cmd[1 : 1+end]
		}
		return cmd[1:]
	}
	if i := strings.IndexAny(cmd, " \t"); i >= 0 {
		return cmd[:i]
	}
	return cmd
}

// launchRows lays out one agent's rows: every mode, one row per command
// (variants), a greyed row for a mode without a command.
func launchRows(c domain.TaskChoice) []launchRowWire {
	var rows []launchRowWire
	for _, mode := range launchModes {
		cmds := c.Interactive
		if mode == launchHeadless {
			cmds = c.Headless
		}
		if len(cmds) == 0 {
			rows = append(rows, launchRowWire{Mode: mode, None: true})
			continue
		}
		for _, tc := range cmds {
			_, err := launchLookPath(commandProgram(tc.Command))
			rows = append(rows, launchRowWire{Mode: mode, Name: tc.Name, Missing: err != nil})
		}
	}
	return rows
}

// launchWait says why a new task would wait ("" = it starts now).
func launchWait(l domain.TaskLoad) string {
	switch {
	case l.SameKey > 0:
		return fmt.Sprintf("%d task(s) with this key ahead — this one will wait", l.SameKey)
	case l.Running >= l.Max:
		return fmt.Sprintf("%d tasks running — it will start when one finishes", l.Running)
	}
	return ""
}

// launchChoiceStore is the promptstate part that remembers the last pick per
// kind (the file store has it; the Store interface does not).
type launchChoiceStore interface {
	TaskLaunchChoice(kind string) (promptstate.TaskLaunch, bool)
	SetTaskLaunchChoice(kind string, c promptstate.TaskLaunch) error
}

func (s *Server) launchStore() launchChoiceStore {
	if ls, ok := s.promptStore().(launchChoiceStore); ok {
		return ls
	}
	return nil
}

func (s *Server) handleTaskLaunch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	t := launchTarget{Kind: q.Get("kind"), Target: q.Get("target"), Branch: q.Get("branch"), Sha: q.Get("sha"), Preview: q.Get("preview"), Shas: splitShas(q.Get("shas"))}
	svc := s.service()
	p, code, err := s.planLaunch(r.Context(), svc, t)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	choices := make([]launchChoiceWire, 0, len(p.choices))
	for _, c := range p.choices {
		choices = append(choices, launchChoiceWire{Agent: c.Agent, Identity: choiceIdentity(c), Rows: launchRows(c)})
	}
	var pick launchPick
	if ls := s.launchStore(); ls != nil {
		if c, ok := ls.TaskLaunchChoice(string(p.kind)); ok {
			pick = launchPick{Identity: c.Agent, Mode: c.Mode, Name: c.Command}
		}
	}
	writeJSON(w, map[string]any{
		"key": p.key, "choices": choices, "pick": pick, "wait": launchWait(domain.Tasks().Load(p.key)),
		"added": p.added, "config_path": config.DefaultGlobalPath(),
	})
}

type taskSubmitReq struct {
	launchTarget
	launchPick
	Approve bool `json:"approve"`
	Cols    int  `json:"cols"`
	Rows    int  `json:"rows"`
}

// handleTaskSubmit runs a pick: the command must be one the dialog offers,
// its program on PATH and the command approved (403 needs_approval with the
// resolved text otherwise); the pick is remembered and the task submitted.
func (s *Server) handleTaskSubmit(w http.ResponseWriter, r *http.Request) {
	var req taskSubmitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	ctx, svc := r.Context(), s.service()
	p, code, err := s.planLaunch(ctx, svc, req.launchTarget)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	tc, err := pickCommand(p.choices, req.launchPick)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, lerr := launchLookPath(commandProgram(tc.Command)); lerr != nil {
		writeErr(w, http.StatusUnprocessableEntity, fmt.Errorf("%s: program not found on PATH", tc.Name))
		return
	}
	if ls := s.launchStore(); ls != nil {
		_ = ls.SetTaskLaunchChoice(string(p.kind), promptstate.TaskLaunch{Agent: req.Identity, Mode: req.Mode, Command: tc.Name})
	}
	key, store := s.toolRepoKey(ctx, svc), s.promptStore()
	hash := promptstate.CommandHash(tc.Command)
	if store == nil || !store.ApprovedToolCommands(key)[hash] {
		if !req.Approve {
			shown := tc.Command
			if p.kind != exttool.CatConflictComplete { // a conflict op resolves after writing its context file
				top, _ := svc.TopLevel(ctx)
				if resolved, rerr := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: top, Range: p.review.Range}); rerr == nil {
					shown = resolved
				}
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "this command has not been approved yet", "needs_approval": true, "tool": tc.Name, "command": shown})
			return
		}
		if store != nil {
			_ = store.ApproveToolCommand(key, hash)
		}
	}
	// The task outlives the request: never the request's context.
	bg := context.Background()
	var spec domain.TaskSpec
	switch p.kind {
	case exttool.CatCommitMessage:
		spec, err = svc.CommitMessageTask(bg, tc)
	case exttool.CatReview:
		spec, err = svc.ReviewTask(bg, tc, p.review)
	case exttool.CatConflictComplete:
		spec, err = svc.ConflictTask(bg, tc, true)
	}
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	if req.Mode != launchHeadless {
		top, _ := svc.TopLevel(ctx)
		cwd, perr := s.sessionPlace(top)
		if perr != nil {
			writeErr(w, http.StatusUnprocessableEntity, perr)
			return
		}
		spec.Cwd = cwd
		spec.Cols, spec.Rows = startSize(req.Cols, req.Rows)
	}
	// An interactive task's agent answers this page's steer inbox (the TUI
	// hands its tasks GG_INBOX the same way: childEnv).
	if inbox := s.steerInbox(); inbox != "" {
		spec.Env = append(spec.Env, "GG_INBOX="+inbox)
	}
	id := domain.Tasks().Submit(spec)
	state := "queued"
	if info, ok := domain.Tasks().Get(id); ok {
		state = string(info.State)
	}
	writeJSON(w, map[string]any{"id": string(id), "key": spec.Key, "state": state})
}

// pickCommand finds the command a pick names among the offered choices; the
// mode must be one the agent has a command for.
func pickCommand(choices []domain.TaskChoice, p launchPick) (config.ToolCommand, error) {
	for _, c := range choices {
		if choiceIdentity(c) != p.Identity {
			continue
		}
		cmds := c.Interactive
		switch p.Mode {
		case launchHeadless:
			cmds = c.Headless
		case launchBackground, launchForeground:
		default:
			return config.ToolCommand{}, fmt.Errorf("no launch mode %q", p.Mode)
		}
		for _, tc := range cmds {
			if tc.Name == p.Name {
				return tc, nil
			}
		}
		return config.ToolCommand{}, fmt.Errorf("%s has no %s command %q", c.Agent, p.Mode, p.Name)
	}
	return config.ToolCommand{}, errors.New("no such agent for this task")
}
