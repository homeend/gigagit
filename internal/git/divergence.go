package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// CountRange counts commits reachable from ref but not from base
// (`git rev-list --count <base>..<ref>`).
func (r *Repo) CountRange(ctx context.Context, base, ref string) (int, error) {
	argv := gitcmd.New("rev-list").Arg("--count", base+".."+ref).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-list --count", argv)
	if err != nil {
		return 0, err
	}
	return parseCount(res.Stdout)
}

// CountRangeUnique counts the commits of base..ref that have NO patch-identical
// counterpart on the base side (`git rev-list --count --cherry-pick
// --right-only <base>...<ref>`). Subtracting it from CountRange tells a remote
// that gained real work from one holding stale copies of commits the caller
// rewrote locally: after a rebase/amend/squash every remote-only commit is a
// patch twin of one of ours, so this returns 0.
func (r *Repo) CountRangeUnique(ctx context.Context, base, ref string) (int, error) {
	argv := gitcmd.New("rev-list").Arg("--count", "--cherry-pick", "--right-only", base+"..."+ref).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-list --count (cherry)", argv)
	if err != nil {
		return 0, err
	}
	return parseCount(res.Stdout)
}

// BranchReflogContains reports whether refs/heads/<branch> ever pointed at
// hash (`git rev-list -g` walks the branch's reflog entries). A branch whose
// reflog cannot be walked — expired, disabled, or never written — reports
// false with no error: the reflog is evidence only when it answers true.
func (r *Repo) BranchReflogContains(ctx context.Context, branch, hash string) (bool, error) {
	argv := gitcmd.New("rev-list").Arg("-g", "refs/heads/"+branch).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-list -g", argv)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, nil
	}
	for _, ln := range strings.Split(res.Stdout, "\n") {
		if strings.TrimSpace(ln) == hash {
			return true, nil
		}
	}
	return false, nil
}

// RemoteBranchTip resolves refs/heads/<branch> on remote WITHOUT fetching any
// object (`git ls-remote --heads <remote> <branch>`), returning "" when the
// remote has no such branch. ls-remote's pattern matches on component
// boundaries (a "main" pattern also reports refs/heads/foo/main), so the exact
// ref name decides.
func (r *Repo) RemoteBranchTip(ctx context.Context, remote, branch string) (string, error) {
	argv := gitcmd.New("ls-remote").Arg("--heads", remote, branch).ToArgv()
	res, err := r.Runner.Run(ctx, "git ls-remote", argv)
	if err != nil {
		return "", err
	}
	want := "refs/heads/" + branch
	for _, ln := range strings.Split(res.Stdout, "\n") {
		sha, ref, ok := strings.Cut(strings.TrimSpace(ln), "\t")
		if ok && strings.TrimSpace(ref) == want {
			return strings.TrimSpace(sha), nil
		}
	}
	return "", nil
}

// ListRemoteHeads lists every branch on remote with its tip hash, without
// fetching any object (`git ls-remote --heads <remote>`). This is a NETWORK
// call; on big remotes the list can run to thousands of rows, but each row is
// one line — no objects move.
func (r *Repo) ListRemoteHeads(ctx context.Context, remote string) ([]model.RemoteHead, error) {
	argv := gitcmd.New("ls-remote").Arg("--heads", remote).ToArgv()
	res, err := r.Runner.Run(ctx, "git ls-remote (heads)", argv)
	if err != nil {
		return nil, err
	}
	var heads []model.RemoteHead
	for _, ln := range strings.Split(res.Stdout, "\n") {
		sha, ref, ok := strings.Cut(strings.TrimSpace(ln), "\t")
		name, isHead := strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/")
		if !ok || !isHead {
			continue
		}
		heads = append(heads, model.RemoteHead{Name: name, Hash: strings.TrimSpace(sha)})
	}
	return heads, nil
}

func parseCount(out string) (int, error) {
	s := strings.TrimSpace(out)
	if s == "" {
		return 0, fmt.Errorf("rev-list --count: empty output")
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("rev-list --count: %q: %w", s, err)
	}
	return n, nil
}
