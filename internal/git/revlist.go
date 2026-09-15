package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// RevListRange lists the commits reachable from tip but not from base
// (`git rev-list <base>..<tip>`), newest first, as full shas. One invocation.
//
// First-parent is deliberately NOT used: a merge preview gathers notes along
// the WHOLE branch, and a note left on a merged side branch still belongs to
// the preview (spec §1.2).
func (r *Repo) RevListRange(ctx context.Context, base, tip string) ([]string, error) {
	argv := gitcmd.New("rev-list").Arg(base + ".." + tip).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-list (range)", argv)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}
