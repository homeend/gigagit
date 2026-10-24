package web

// The conflict window's in-place tools (the TUI's conflict process t picker):
// per-file mergetools and commands that ask for <user:…> input. gg web has no
// terminal to hand over, so the tool runs in a session the console shows;
// when it ends, done says whether the merged file changed (the page offers
// to mark it resolved) and returns a resolve & complete tool's overview.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/template"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/conflict/tools", s.handleConflictTools)
		mux.HandleFunc("POST /api/conflict/tool", writeGuard(s.handleConflictTool))
		mux.HandleFunc("POST /api/conflict/tool/done", writeGuard(s.handleConflictToolDone))
	})
}

type conflictToolRow struct {
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Mode     string   `json:"mode"`
	PerFile  bool     `json:"per_file"`
	Labels   []string `json:"labels"`
}

// toolRun is a started in-place tool. A watcher finishes it when its session
// ends — the outcome recorded, the temp files gone — whether or not a page
// asks; done hands the outcome to the first page that does.
type toolRun struct {
	session     domain.SessionID
	finished    bool
	outcome     map[string]any // set when finished
	name        string
	file        string // per-file: the repo-relative conflicted path
	merged      string // per-file: its absolute path
	preMtime    time.Time
	perFile     bool
	messageFile string   // resolve & complete: the overview file
	temps       []string // the context file, the script
	cleanup     func()   // the per-file quartet
}

// toolRuns are the in-place tool runs by session: the sessions list marks
// them (isToolRun), so any tab — a reloaded one too — can collect one.
var (
	toolRunsMu sync.Mutex
	toolRuns   = map[domain.SessionID]*toolRun{}
)

// toolRunKeep is how long a finished, uncollected outcome waits for a page.
const toolRunKeep = 30 * time.Minute

// isToolRun reports a session that is an in-place tool's run.
func isToolRun(id domain.SessionID) bool {
	toolRunsMu.Lock()
	defer toolRunsMu.Unlock()
	_, ok := toolRuns[id]
	return ok
}

// webToolCommands are kind's valid commands offered on the web.
func webToolCommands(cfg config.Config, kind exttool.Category) []config.ToolCommand {
	var out []config.ToolCommand
	for _, tc := range cfg.Tools.Command {
		if tc.Category != string(kind) || !config.ToolVisibleIn(tc, "web") {
			continue
		}
		if config.ValidateToolCommand(tc) != nil || template.ValidateCommandTokens(tc.Command, tc.PerFile) != nil {
			continue
		}
		out = append(out, tc)
	}
	return out
}

// conflictPlace is the paused operation and the focused file.
type conflictPlace struct {
	state   domain.ConflictState
	files   []model.FileStatus
	focused *model.FileStatus
	top     string
}

func (s *Server) conflictPlace(ctx context.Context, svc *domain.Service, file string) (conflictPlace, error) {
	st, err := svc.Status(ctx)
	if err != nil {
		return conflictPlace{}, err
	}
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return conflictPlace{}, err
	}
	p := conflictPlace{state: svc.Conflict(ctx, st), files: st.Conflicts(), top: top}
	for i := range p.files {
		if file != "" && p.files[i].Path == file {
			p.focused = &p.files[i]
		}
	}
	return p, nil
}

// inPlaceTools is the TUI's conflictPickerRows minus the agents: conflict
// and resolve & complete commands that run in place — a per-file mergetool
// (on a focused both-sides file) or a command asking for input.
func inPlaceTools(cfg config.Config, p conflictPlace) []config.ToolCommand {
	both := p.focused != nil && p.focused.ConflictClass() == model.ConflictBothSides
	inPlace := func(tc config.ToolCommand) bool { return tc.PerFile || len(template.UserLabels(tc.Command)) > 0 }
	var out []config.ToolCommand
	for _, tc := range domain.ConflictToolChoices(webToolCommands(cfg, exttool.CatConflict), p.state.Op, both) {
		if inPlace(tc) {
			out = append(out, tc)
		}
	}
	for _, tc := range domain.CompleteToolChoices(webToolCommands(cfg, exttool.CatConflictComplete), p.state.Op) {
		if inPlace(tc) {
			out = append(out, tc)
		}
	}
	return out
}

func (s *Server) handleConflictTools(w http.ResponseWriter, r *http.Request) {
	ctx, svc := r.Context(), s.service()
	p, err := s.conflictPlace(ctx, svc, r.URL.Query().Get("file"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	cfg, err := s.effectiveConfig(ctx, svc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := []conflictToolRow{}
	for _, tc := range inPlaceTools(cfg, p) {
		labels := template.UserLabels(tc.Command)
		if labels == nil {
			labels = []string{}
		}
		rows = append(rows, conflictToolRow{Name: tc.Name, Category: tc.Category, Mode: tc.Mode, PerFile: tc.PerFile, Labels: labels})
	}
	writeJSON(w, map[string]any{"tools": rows})
}

type conflictToolReq struct {
	Name     string            `json:"name"`
	Category string            `json:"category"`
	File     string            `json:"file"`
	Inputs   map[string]string `json:"inputs"`
	Approve  bool              `json:"approve"`
	Cols     int               `json:"cols"`
	Rows     int               `json:"rows"`
}

// handleConflictTool starts an in-place tool: the command must be one the
// picker offers, every <user:…> value given, the command approved (403
// needs_approval with the resolved text otherwise — the temp files go); it
// then runs in a session and answers {token, session}.
func (s *Server) handleConflictTool(w http.ResponseWriter, r *http.Request) {
	var req conflictToolReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	ctx, svc := r.Context(), s.service()
	p, err := s.conflictPlace(ctx, svc, req.File)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	cfg, err := s.effectiveConfig(ctx, svc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var tc config.ToolCommand
	found := false
	for _, c := range inPlaceTools(cfg, p) {
		if c.Name == req.Name && c.Category == req.Category {
			tc, found = c, true
			break
		}
	}
	if !found {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("no in-place tool %q here", req.Name))
		return
	}
	for _, l := range template.UserLabels(tc.Command) {
		if _, ok := req.Inputs[l]; !ok {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("%s needs a value for %q", tc.Name, l))
			return
		}
	}
	run := &toolRun{name: tc.Name, perFile: tc.PerFile}
	discard := func() {
		for _, f := range run.temps {
			os.Remove(f)
		}
		if run.cleanup != nil {
			run.cleanup()
		}
		if run.messageFile != "" {
			os.Remove(run.messageFile)
		}
	}
	cctx := template.CmdCtx{Op: p.state.Op, Source: p.state.Source, Target: p.state.Target, Repo: p.top}
	for _, f := range p.files {
		cctx.ConflictedFiles = append(cctx.ConflictedFiles, f.Path)
	}
	ctxFile, err := domain.WriteToolContextFile(cctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	run.temps = append(run.temps, ctxFile)
	cctx.ContextFile = ctxFile
	if tc.PerFile {
		run.file, run.merged = p.focused.Path, filepath.Join(p.top, p.focused.Path)
		cctx.File, cctx.Merged = run.file, run.merged
		local, base, remote, cleanup, verr := svc.ConflictFileVersions(context.Background(), run.file, p.focused.ConflictHasBase())
		if verr != nil {
			discard()
			writeErr(w, http.StatusUnprocessableEntity, verr)
			return
		}
		run.cleanup = cleanup
		cctx.Local, cctx.Base, cctx.Remote = local, base, remote
	}
	resolved, err := template.ResolveCommand(tc.Command, req.Inputs, cctx)
	if err != nil {
		discard()
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	env := domain.ToolEnv(cctx)
	if tc.Category == string(exttool.CatConflictComplete) {
		mf, merr := os.CreateTemp("", "gg-overview-*.md")
		if merr != nil {
			discard()
			writeErr(w, http.StatusInternalServerError, merr)
			return
		}
		mf.Close()
		run.messageFile = mf.Name()
		env = append(env, "GG_MESSAGE_FILE="+run.messageFile, "GG_TASK=conflict_complete")
	}
	key, store := s.toolRepoKey(ctx, svc), s.promptStore()
	hash := promptstate.CommandHash(tc.Command)
	if store == nil || !store.ApprovedToolCommands(key)[hash] {
		if !req.Approve {
			discard()
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "this command has not been approved yet", "needs_approval": true, "tool": tc.Name, "command": resolved})
			return
		}
		if store != nil {
			_ = store.ApproveToolCommand(key, hash)
		}
	}
	script, err := domain.WriteToolScript(resolved)
	if err != nil {
		discard()
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	run.temps = append(run.temps, script)
	cwd, err := s.sessionPlace(p.top)
	if err != nil {
		discard()
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	if run.merged != "" {
		if fi, serr := os.Stat(run.merged); serr == nil {
			run.preMtime = fi.ModTime()
		}
	}
	cols, rows := startSize(req.Cols, req.Rows)
	// The session outlives the request: never the request's context.
	sess, err := svc.StartToolSession(context.Background(), tc.Name, script, p.top, cwd, cols, rows, env)
	if err != nil {
		discard()
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	run.session = sess.Info().ID
	toolRunsMu.Lock()
	toolRuns[run.session] = run
	toolRunsMu.Unlock()
	go finishToolRun(run, sess)
	writeJSON(w, map[string]any{"session": string(run.session)})
}

// finishToolRun waits for the tool's session to end and records the outcome
// the TUI's toolFinished derives: the exit, whether it was an interrupt
// (130/143, or the process killed by a signal — ctrl-C in the console, X),
// whether a per-file run changed its file, a clean exit's overview. The temp
// files go at once; the outcome waits toolRunKeep for a page.
func finishToolRun(run *toolRun, sess *domain.AgentSession) {
	<-sess.Done()
	exit := sess.Info().ExitCode
	interrupted := exit == 130 || exit == 143 || exit == -1
	failed := exit != 0 && !interrupted
	changed := false
	if run.merged != "" {
		if fi, err := os.Stat(run.merged); err == nil && fi.ModTime().After(run.preMtime) {
			changed = true
		}
	}
	overview := ""
	if run.messageFile != "" {
		if exit == 0 {
			if b, err := os.ReadFile(run.messageFile); err == nil {
				overview = string(b)
			}
		}
		os.Remove(run.messageFile)
	}
	for _, f := range run.temps {
		os.Remove(f)
	}
	if run.cleanup != nil {
		run.cleanup()
	}
	toolRunsMu.Lock()
	run.finished = true
	run.outcome = map[string]any{
		"name": run.name, "file": run.file, "per_file": run.perFile, "changed": changed, "overview": overview,
		"exit_code": exit, "interrupted": interrupted, "failed": failed, "complete": run.messageFile != "",
	}
	toolRunsMu.Unlock()
	time.AfterFunc(toolRunKeep, func() {
		toolRunsMu.Lock()
		if toolRuns[run.session] == run {
			delete(toolRuns, run.session)
		}
		toolRunsMu.Unlock()
	})
}

// handleConflictToolDone hands a finished run's outcome to the first page
// that asks (409 while the tool still runs, 404 once collected or unknown).
func (s *Server) handleConflictToolDone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Session string `json:"session"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	id := domain.SessionID(req.Session)
	toolRunsMu.Lock()
	run, ok := toolRuns[id]
	var outcome map[string]any
	if ok && run.finished {
		outcome = run.outcome
		delete(toolRuns, id)
	}
	toolRunsMu.Unlock()
	switch {
	case !ok:
		writeErr(w, http.StatusNotFound, errors.New("no such tool run"))
	case outcome == nil:
		writeErr(w, http.StatusConflict, errors.New("the tool is still running"))
	default:
		writeJSON(w, outcome)
	}
}
