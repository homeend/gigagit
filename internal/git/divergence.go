package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
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
