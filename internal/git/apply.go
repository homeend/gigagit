package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"

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

// PatchPaths returns the paths the patch file at path touches
// (`git apply --numstat -z <path>`) — it reads the patch only, touching
// neither the working tree nor the index. Record shape: ordinarily one
// "added\tdeleted\tpath\x00" record per file, same as DiffNumstat/
// ParseNumstat. The parser also recognizes `git diff --numstat -z`'s rename
// shape (empty path field, next two NUL fields are old/new) defensively, but
// empirically (verified against git 2.43) `git apply --numstat -z` does NOT
// use it for a rename: it emits a single record naming only the NEW path,
// even when the patch's diff --git header carries "rename from"/"rename to".
// That is a known gap for this verb's sole caller, engine.ApplyPatch's
// plain-apply-failed/--3way-succeeded fallback: PatchPaths is used to
// surgically unstage what --3way's implied --index staged, but for a
// renamed file it can only unstage the new path — the old path's staged
// deletion is left behind (harmless but not fully "unstaged": `git status`
// shows `D <old>` / `?? <new>` instead of the rename disappearing from the
// index). One invocation.
func (r *Repo) PatchPaths(ctx context.Context, path string) ([]string, error) {
	b := gitcmd.New("apply").Arg("--numstat", "-z", path)
	res, err := r.Runner.Run(ctx, "git apply", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return parsePatchNumstatPaths(res.Stdout), nil
}

// parsePatchNumstatPaths parses `git apply --numstat -z` output into the
// list of touched paths, per the record shape documented on PatchPaths.
func parsePatchNumstatPaths(out string) []string {
	fields := strings.Split(out, "\x00")
	var paths []string
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		parts := strings.SplitN(f, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		p := parts[2]
		if p == "" { // rename: the next two fields are old, new
			if i+2 >= len(fields) || fields[i+1] == "" || fields[i+2] == "" {
				break
			}
			paths = append(paths, fields[i+1], fields[i+2])
			i += 2
			continue
		}
		paths = append(paths, p)
	}
	return paths
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
