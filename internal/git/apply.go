package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// IsMailboxPatch reports whether data (the head of a patch file) is a git
// format-patch mailbox: its first non-empty line starts with "From " — the
// mbox From_ sentinel git mailsplit keys on. A plain `git diff` starts with
// "diff --git" (other unified diffs with "---") and is not a mailbox. Pure;
// no git invocation.
func IsMailboxPatch(data []byte) bool {
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		return bytes.HasPrefix(line, []byte("From "))
	}
	return false
}

// ApplyPatch applies the unified diff at path to the working tree
// (`git apply [--3way] <path>`; no --index/--cached, so changes land
// unstaged). With threeWay a hunk that misses falls back to a 3-way merge and
// may leave conflict markers + unmerged index entries — git exits non-zero in
// that case TOO, so the error alone cannot distinguish applied-with-conflicts
// from applied-nothing; callers probe status (see engine.ApplyPatch). One
// invocation.
func (r *Repo) ApplyPatch(ctx context.Context, path string, threeWay bool) error {
	b := gitcmd.New("apply").ArgIf(threeWay, "--3way").Arg(path)
	_, err := r.Runner.Run(ctx, "git apply", b.ToArgv())
	return err
}

// AmMailbox applies a format-patch mailbox as real commits
// (`git am [--3way] <path>`), preserving each patch's author, date, and
// message. On conflict git am stops mid-way with rebase-apply/ state on disk;
// callers roll back via AmAbort (engine.ApplyPatch keeps am atomic — gg does
// not model a paused am). One invocation.
func (r *Repo) AmMailbox(ctx context.Context, path string, threeWay bool) error {
	b := gitcmd.New("am").ArgIf(threeWay, "--3way").Arg(path)
	_, err := r.Runner.Run(ctx, "git am", b.ToArgv())
	return err
}

// AmAbort rolls back an in-progress git am (`git am --abort`), restoring the
// branch to its pre-am state — including commits already made from earlier
// patches in a multi-patch mailbox. One invocation.
func (r *Repo) AmAbort(ctx context.Context) error {
	_, err := r.Runner.Run(ctx, "git am --abort", gitcmd.New("am").Arg("--abort").ToArgv())
	return err
}

// AmInProgress reports whether a git am is mid-flight: rebase-apply/applying
// exists — the am-specific marker. A paused REBASE on the apply backend has
// rebase-apply/rebasing instead, and a bare rebase-apply dir belongs to it,
// NOT to am: this guard is what keeps engine.ApplyPatch's rollback from
// running `git am --abort` on top of (and thereby aborting) a user's paused
// rebase. One git invocation (GitDir) + one stat. Deliberately NOT part of
// PausedOpIn — gg does not model paused am.
func (r *Repo) AmInProgress(ctx context.Context) (bool, error) {
	dir, err := r.GitDir(ctx)
	if err != nil {
		return false, err
	}
	_, statErr := os.Stat(filepath.Join(dir, "rebase-apply", "applying"))
	return statErr == nil, nil
}
