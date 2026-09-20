package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// RemoteDefaultBranch returns the branch a remote's HEAD points at, in its
// remote-tracking spelling ("origin/main") — `git symbolic-ref --quiet --short
// refs/remotes/<remote>/HEAD`. One invocation. "" with no error when the remote
// has no HEAD recorded here, which is common and not a failure: `git clone`
// writes it, `git remote add` + fetch does not.
func (r *Repo) RemoteDefaultBranch(ctx context.Context, remote string) (string, error) {
	argv := gitcmd.New("symbolic-ref").Arg("--quiet", "--short", "refs/remotes/"+remote+"/HEAD").ToArgv()
	res, err := r.Runner.Run(ctx, "git symbolic-ref (remote HEAD)", argv)
	if err != nil {
		if res.ExitCode == 1 { // not a symbolic ref / no such ref — CurrentBranch's convention
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
