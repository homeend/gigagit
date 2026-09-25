package engine

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/repogate"
	"github.com/homeend/gigagit/internal/template"
)

// CompleteConflict runs a resolve-and-complete agent headless against the
// currently paused sequencer operation: the agent resolves every conflict,
// stages, runs the matching --continue itself (it OWNS the sequencer for the
// run — the CatConflictComplete contract), and reports an overview. The op
// writes a context file (op/source/target + conflicted paths, C-quoted — the
// exact bytes the TUI's tool runs write) and an empty $GG_MESSAGE_FILE, then
// resolves op.Command ITSELF (unlike ReviewChanges, which takes a resolved
// command: a custom <context-file> token needs the real temp path, which
// exists only here) and runs it via the CaptureRunner. Output-channel
// contract as ever: non-empty $GG_MESSAGE_FILE wins over stdout; an empty
// overview is NOT an error (the TUI's "reported no overview" stance).
//
// LockMode is Read — deliberately: the AGENT mutates the tree and refs, not
// gg; gg only reads. Read keeps other frontends' reads (status, commits)
// alive during a minutes-long run while still excluding gg's own tree- and
// ref-writing ops. The TUI precedent for this category is no reservation at
// all ($EDITOR standing); Read is strictly safer. Validation ("is anything
// paused?") is the domain wrapper's job, the ReviewChanges split.
type CompleteConflict struct {
	Command         string   // command TEMPLATE text (config); resolved by the op
	Dir             string   // worktree root the agent runs in
	Env             []string // caller env additions
	Op              string   // paused op: merge|rebase|cherry-pick|revert
	Source          string   // the op's parties (context values, not executed)
	Target          string
	ConflictedFiles []string // repo-relative conflicted paths
}

// ConflictAgent runs a whole-operation conflict agent under the
// CatConflict contract — resolve and stage, never --continue (gg's
// ContinueOp owns the sequencer). Inputs and LockMode as CompleteConflict;
// GG_TASK=conflict. Its result (a summary the agent may write to
// $GG_MESSAGE_FILE) is informational: the outcome is the repository state.
type ConflictAgent struct {
	Command         string   // command TEMPLATE text (config); resolved by Prepare
	Dir             string   // worktree root the agent runs in
	Env             []string // caller env additions
	Op              string   // paused op: merge|rebase|cherry-pick|revert
	Source          string
	Target          string
	ConflictedFiles []string
}

var (
	_ CaptureTask = CompleteConflict{}
	_ CaptureTask = ConflictAgent{}
)

func (op CompleteConflict) LockMode() repogate.Mode { return repogate.Read }
func (op ConflictAgent) LockMode() repogate.Mode    { return repogate.Read }

func (op CompleteConflict) Prepare(_ context.Context, _ OpDeps) (TaskInputs, error) {
	return prepareConflictAgent(op.Command, op.Dir, op.Env, op.Op, op.Source, op.Target, op.ConflictedFiles, "conflict_complete")
}

func (op ConflictAgent) Prepare(_ context.Context, _ OpDeps) (TaskInputs, error) {
	return prepareConflictAgent(op.Command, op.Dir, op.Env, op.Op, op.Source, op.Target, op.ConflictedFiles, "conflict")
}

func (op CompleteConflict) Collect(in TaskInputs, stdout []byte) (Result, error) {
	return Result{Captured: collectCaptured(in.MessageFile, stdout)}.WithSummary("conflict agent finished (%s)", op.Op), nil
}

func (op ConflictAgent) Collect(in TaskInputs, stdout []byte) (Result, error) {
	return Result{Captured: collectCaptured(in.MessageFile, stdout)}.WithSummary("conflict agent finished (%s)", op.Op), nil
}

func (op CompleteConflict) Run(ctx context.Context, deps OpDeps) (Result, error) {
	return runCaptureTask(ctx, deps, op)
}

func (op ConflictAgent) Run(ctx context.Context, deps OpDeps) (Result, error) {
	return runCaptureTask(ctx, deps, op)
}

// prepareConflictAgent writes the context doc (op/source/target + the
// C-quoted conflicted paths — the bytes the TUI's tool runs write) and an
// empty $GG_MESSAGE_FILE, then resolves the command template against them:
// a custom <context-file> token needs the real temp path, which exists only
// here.
func prepareConflictAgent(command, dir string, extra []string, opName, source, target string, files []string, task string) (TaskInputs, error) {
	tmp := &tempSet{}
	fail := func(err error) (TaskInputs, error) { tmp.cleanup(); return TaskInputs{}, err }
	ctxPath, err := tmp.write("gg-context-*.txt", template.ConflictContextDoc(opName, source, target, files))
	if err != nil {
		return fail(err)
	}
	msgPath, err := tmp.write("gg-overview-*.md", "")
	if err != nil {
		return fail(err)
	}
	resolved, err := template.ResolveCommand(command, nil, template.CmdCtx{
		Op: opName, Source: source, Target: target,
		ConflictedFiles: files, Repo: dir, ContextFile: ctxPath,
	})
	if err != nil {
		return fail(err)
	}
	env := append(append([]string{}, extra...),
		"GG_OP="+opName,
		"GG_SOURCE="+source,
		"GG_TARGET="+target,
		"GG_CONFLICTED_FILES="+strings.Join(files, " "),
		"GG_REPO="+dir,
		"GG_FILE=", "GG_LOCAL=", "GG_BASE=", "GG_REMOTE=", "GG_MERGED=",
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_MESSAGE_FILE="+msgPath,
		"GG_TASK="+task,
	)
	return TaskInputs{Command: resolved, Dir: dir, Env: env, MessageFile: msgPath, Cleanup: tmp.cleanup}, nil
}
