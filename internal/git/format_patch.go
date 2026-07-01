package git

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ParentCount returns how many parents rev has: 0 for the root commit, 1 for a
// normal commit, ≥2 for a merge. `git rev-list --parents --max-count=1 <rev>`
// prints "<rev> <p1> <p2>…"; the parent count is the field count minus one. One
// invocation. Used to refuse patch export of a merge commit (format-patch -1 on
// a merge silently emits a different commit's patch).
func (r *Repo) ParentCount(ctx context.Context, rev string) (int, error) {
	argv := gitcmd.New("rev-list").Arg("--parents", "--max-count=1", rev).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-list (parent count)", argv)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(strings.TrimSpace(res.Stdout))
	if len(fields) == 0 {
		return 0, fmt.Errorf("rev-list --parents %s: empty output", rev)
	}
	return len(fields) - 1, nil
}

// FormatPatch returns the mailbox-format patch for the single commit rev
// (`git format-patch -1 --binary --stdout <rev> [-- <paths…>]`). With paths the
// diff is scoped to those files while keeping the commit's From/Subject header;
// --binary keeps genuinely-binary changes appliable. The output is a git am-able
// patch. One invocation. Callers must reject merge commits first (see
// ParentCount): format-patch -1 skips a merge and emits the wrong commit.
func (r *Repo) FormatPatch(ctx context.Context, rev string, paths ...string) ([]byte, error) {
	b := gitcmd.New("format-patch").Arg("-1", "--binary", "--stdout", rev)
	if len(paths) > 0 {
		b = b.Arg("--")
		for _, p := range paths {
			b = b.Arg(p)
		}
	}
	res, err := r.Runner.Run(ctx, "git format-patch", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}
