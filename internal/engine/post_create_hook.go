package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HookDecisionID is the DecisionRequest ID for approving a post-create hook.
// The hook runs only when the decider answers "run"; any other answer — or no
// decider at all — skips it (a repo .gg.toml hook can be clone-borne, so the
// safe default is never to run unattended).
const HookDecisionID = "post_create_hook.run"

// hookPromptPreview bounds the script shown in the approval prompt so a long
// script cannot overflow the TUI modal; the full script always lives in .gg.toml.
func hookPromptPreview(script string) string {
	const maxLines = 40
	lines := strings.Split(strings.TrimRight(script, "\n"), "\n")
	if len(lines) <= maxLines {
		return strings.TrimRight(script, "\n")
	}
	return strings.Join(lines[:maxLines], "\n") +
		fmt.Sprintf("\n… (%d more lines — see .gg.toml)", len(lines)-maxLines)
}

// runPostCreateHook runs the configured post-create hook in the new worktree.
// It is non-fatal: the worktree already exists, so a hook error/non-zero exit is
// surfaced (a GitLine plus a returned Summary suffix) but never fails the op.
// Returns "" on success or when no hook is configured.
func runPostCreateHook(ctx context.Context, deps OpDeps, worktreePath, branch, script string) string {
	if strings.TrimSpace(script) == "" {
		return ""
	}
	resp, derr := deps.decide(ctx, DecisionRequest{
		ID:      HookDecisionID,
		Prompt:  "Run this post-create hook?\n\n" + hookPromptPreview(script),
		Options: []string{"run", "skip"},
	})
	if derr != nil || resp.Option != "run" {
		deps.emit(ctx, GitLine{Raw: "post-create hook skipped"})
		return ""
	}
	main, _ := mainWorktreeRoot(ctx, deps) // best-effort; "" if unavailable
	env := append(os.Environ(),
		"GG_MAIN_WORKTREE="+main,
		"GG_WORKTREE_PATH="+worktreePath,
		"GG_BRANCH="+branch,
		"GG_REPO="+filepath.Base(main),
	)
	deps.emit(ctx, Progress{Step: "running post-create hook", Detail: worktreePath})
	code, err := deps.hookRunner().Run(ctx, HookSpec{Dir: worktreePath, Env: env, Script: script},
		func(line string) { deps.emit(ctx, GitLine{Raw: line}) })
	switch {
	case err != nil:
		deps.emit(ctx, GitLine{Raw: "post-create hook error: " + err.Error()})
		return " (post-create hook error: " + err.Error() + ")"
	case code != 0:
		msg := fmt.Sprintf("post-create hook exited with code %d", code)
		deps.emit(ctx, GitLine{Raw: msg})
		return fmt.Sprintf(" (post-create hook failed: exit %d)", code)
	}
	return ""
}
