package domain

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/repogate"
	"github.com/homeend/gigagit/internal/template"
)

// TaskSpec is one AI task to submit to Tasks(). A kind builder
// (CommitMessageTask, ReviewTask, ConflictTask) fills the identity, the op
// and the result hooks; the frontend adds Env (its GG_INBOX), Cwd (a
// translated worktree path) and the console size.
type TaskSpec struct {
	Key      string           // "<kind> — <target>": identity and title (ruling 6)
	Kind     exttool.Category // commit_message | review | conflict | conflict_complete
	Agent    string           // display name, e.g. "Claude Code"
	AgentID  string           // exttool tool id, "" for a custom command
	Mode     TaskMode
	Repo     string // repository display name
	Worktree string // worktree directory: the session's identity
	Cwd      string // where the process runs when it differs from Worktree
	Env      []string
	Cols     int
	Rows     int
	// Svc is the Service the task was submitted from. It runs (headless) or
	// prepares (interactive) the op even after the frontend has switched to
	// another worktree.
	Svc *Service
	Op  engine.CaptureTask
	// Parse shapes a collected capture into the result text; an error or an
	// empty text is "no result". nil = strings.TrimSpace.
	Parse func(captured string) (string, error)
	// ResultOptional: a run that exits 0 without a result is done, not
	// failed — a conflict run's outcome is the repository state.
	ResultOptional bool
}

var longHex = regexp.MustCompile(`\b[0-9a-f]{8,40}\b`)

func sha7(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// worktreeName is the directory's base name in either notation.
func worktreeName(dir string) string {
	return path.Base(strings.ReplaceAll(dir, `\`, "/"))
}

func CommitMessageKey(worktree, head string) string {
	return "commit message — " + worktreeName(worktree) + " @ " + sha7(head)
}

func ReviewKey(worktree string, t ReviewTarget) string {
	if strings.TrimSpace(t.Range) == "" {
		return "review — " + worktreeName(worktree) + " working changes"
	}
	return "review — " + longHex.ReplaceAllStringFunc(t.Range, sha7)
}

func ConflictKey(worktree, op, head string) string {
	return "resolve conflict — " + worktreeName(worktree) + " " + op + " " + sha7(head)
}

func CompleteKey(worktree, op, head string) string {
	return "resolve & complete — " + worktreeName(worktree) + " " + op + " " + sha7(head)
}

// taskBase fills the fields every kind shares.
func (s *Service) taskBase(ctx context.Context, tc config.ToolCommand, kind exttool.Category) (TaskSpec, string, error) {
	mode, ok := TaskModeOf(tc)
	if !ok || tc.Category != string(kind) {
		return TaskSpec{}, "", fmt.Errorf("%s cannot run as a %s task", tc.Name, kind)
	}
	top, err := s.TopLevel(ctx)
	if err != nil {
		return TaskSpec{}, "", err
	}
	repo, err := s.RepoName(ctx)
	if err != nil || repo == "" {
		repo = worktreeName(top)
	}
	return TaskSpec{
		Kind: kind, Agent: AgentName(tc), AgentID: agentIDFor(tc), Mode: mode,
		Repo: repo, Worktree: top, Svc: s,
	}, top, nil
}

// CommitMessageTask builds a commit-message task over the staged changes.
func (s *Service) CommitMessageTask(ctx context.Context, tc config.ToolCommand) (TaskSpec, error) {
	spec, top, err := s.taskBase(ctx, tc, exttool.CatCommitMessage)
	if err != nil {
		return TaskSpec{}, err
	}
	head, err := s.RevParse(ctx, "HEAD")
	if err != nil {
		return TaskSpec{}, err
	}
	resolved, err := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: top})
	if err != nil {
		return TaskSpec{}, err
	}
	spec.Key = CommitMessageKey(top, head)
	spec.Op = engine.GenerateMessage{Command: resolved, Dir: top, Env: []string{"GG_TASK=commit_message"}}
	spec.Parse = parseCommitMessage
	return spec, nil
}

// parseCommitMessage normalises any catalogue tool's output to
// "subject\n\nbody" (or just the subject).
func parseCommitMessage(captured string) (string, error) {
	subject, body, err := exttool.ParseCaptureMessage([]byte(captured))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(subject) == "" {
		return "", errors.New("empty commit message")
	}
	if strings.TrimSpace(body) == "" {
		return subject, nil
	}
	return subject + "\n\n" + body, nil
}

// ReviewTask builds a review task over target. notesFile as ReviewReportNotes.
func (s *Service) ReviewTask(ctx context.Context, tc config.ToolCommand, target ReviewTarget, notesFile string) (TaskSpec, error) {
	spec, top, err := s.taskBase(ctx, tc, exttool.CatReview)
	if err != nil {
		return TaskSpec{}, err
	}
	resolved, err := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Range: target.Range, Repo: top})
	if err != nil {
		return TaskSpec{}, err
	}
	spec.Key = ReviewKey(top, target)
	spec.Op = engine.ReviewChanges{
		Command: resolved, Dir: top, Env: []string{"GG_TASK=review"},
		Diff: target.Diff, RangeLabel: target.DisplayLabel(), NotesFile: notesFile,
	}
	spec.Parse = parseReport
	return spec, nil
}

// parseReport unwraps a JSON-enveloped report; an empty one is an error.
func parseReport(captured string) (string, error) {
	report, err := exttool.ParseCaptureReport(captured)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(report) == "" {
		return "", errors.New("empty report")
	}
	return strings.TrimSpace(report), nil
}

// ConflictTask builds a whole-operation conflict task against the paused
// operation: resolve & complete when complete, else resolve (never
// --continue). Refuses when nothing is paused.
func (s *Service) ConflictTask(ctx context.Context, tc config.ToolCommand, complete bool) (TaskSpec, error) {
	kind := exttool.CatConflict
	if complete {
		kind = exttool.CatConflictComplete
	}
	spec, top, err := s.taskBase(ctx, tc, kind)
	if err != nil {
		return TaskSpec{}, err
	}
	st, err := s.Status(ctx)
	if err != nil {
		return TaskSpec{}, err
	}
	cs := s.Conflict(ctx, st)
	if cs.Op == "" {
		return TaskSpec{}, errors.New("no paused operation to resolve")
	}
	head, err := s.RevParse(ctx, "HEAD")
	if err != nil {
		return TaskSpec{}, err
	}
	files := unmergedPaths(st)
	if complete {
		spec.Key = CompleteKey(top, cs.Op, head)
		spec.Op = engine.CompleteConflict{Command: tc.Command, Dir: top, Op: cs.Op, Source: cs.Source, Target: cs.Target, ConflictedFiles: files}
	} else {
		spec.Key = ConflictKey(top, cs.Op, head)
		spec.Op = engine.ConflictAgent{Command: tc.Command, Dir: top, Op: cs.Op, Source: cs.Source, Target: cs.Target, ConflictedFiles: files}
	}
	spec.Parse = parseReport
	spec.ResultOptional = true
	return spec, nil
}

// PrepareTask runs op.Prepare under the op's reservation (the gate Execute
// uses), so the inputs are a consistent read of the repository. The temp
// files outlive the reservation; the caller runs in.Cleanup.
func (s *Service) PrepareTask(ctx context.Context, op engine.CaptureTask) (engine.TaskInputs, error) {
	mode := repogate.Read
	if lm, ok := op.(lockModer); ok {
		mode = lm.LockMode()
	}
	res, err := s.gateFor(ctx).Acquire(ctx, mode, "prepare "+engine.OpName(op))
	if err != nil {
		return engine.TaskInputs{}, err
	}
	defer res.Release()
	return op.Prepare(ctx, engine.OpDeps{Repo: s.repo})
}
