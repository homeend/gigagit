package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runPostCreateHook runs the configured post-create hook in the new worktree.
// It is non-fatal: the worktree already exists, so a hook error/non-zero exit is
// surfaced (a GitLine plus a returned Summary suffix) but never fails the op.
// Returns "" on success or when no hook is configured.
func runPostCreateHook(ctx context.Context, deps OpDeps, worktreePath, branch, script string) string {
	if strings.TrimSpace(script) == "" {
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
