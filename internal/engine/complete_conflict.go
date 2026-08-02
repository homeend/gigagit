package engine

import (
	"context"
	"os"
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

var _ Operation = CompleteConflict{}

func (op CompleteConflict) LockMode() repogate.Mode { return repogate.Read }

func (op CompleteConflict) Run(ctx context.Context, deps OpDeps) (Result, error) {
	ctxPath, err := writeTempFile("gg-context-*.txt",
		template.ConflictContextDoc(op.Op, op.Source, op.Target, op.ConflictedFiles))
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(ctxPath)
	msgPath, err := writeTempFile("gg-overview-*.md", "")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(msgPath)

	resolved, err := template.ResolveCommand(op.Command, nil, template.CmdCtx{
		Op: op.Op, Source: op.Source, Target: op.Target,
		ConflictedFiles: op.ConflictedFiles, Repo: op.Dir, ContextFile: ctxPath,
	})
	if err != nil {
		return Result{}, err
	}

	env := append(append([]string{}, os.Environ()...), op.Env...)
	env = append(env,
		"GG_OP="+op.Op,
		"GG_SOURCE="+op.Source,
		"GG_TARGET="+op.Target,
		"GG_CONFLICTED_FILES="+strings.Join(op.ConflictedFiles, " "),
		"GG_REPO="+op.Dir,
		"GG_FILE=", "GG_LOCAL=", "GG_BASE=", "GG_REMOTE=", "GG_MERGED=",
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_MESSAGE_FILE="+msgPath,
		"GG_TASK=conflict_complete",
	)
	stdout, runErr := deps.captureRunner().Capture(ctx,
		CaptureSpec{Dir: op.Dir, Env: env, Command: resolved},
		func(line string) { deps.emit(ctx, GitLine{Raw: line}) })
	captured := string(stdout)
	if fileMsg, rerr := os.ReadFile(msgPath); rerr == nil && strings.TrimSpace(string(fileMsg)) != "" {
		captured = string(fileMsg)
	}
	if runErr != nil {
		return Result{Captured: captured}, runErr
	}
	return Result{Captured: captured}.WithSummary("conflict agent finished (%s)", op.Op), nil
}
