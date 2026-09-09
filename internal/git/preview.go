package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// CountLeftRight counts the commits only on each side of a symmetric range
// (`git rev-list --left-right --count <left>...<right>`): leftOnly is on left
// but not right, rightOnly the reverse. For a merge preview left = target,
// right = source, so rightOnly is "commits ahead". One invocation.
func (r *Repo) CountLeftRight(ctx context.Context, left, right string) (leftOnly, rightOnly int, err error) {
	argv := gitcmd.New("rev-list").Arg("--left-right", "--count", left+"..."+right).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-list --left-right --count", argv)
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("rev-list --left-right --count: %q", strings.TrimSpace(res.Stdout))
	}
	if leftOnly, err = strconv.Atoi(fields[0]); err != nil {
		return 0, 0, fmt.Errorf("rev-list --left-right --count: %q: %w", fields[0], err)
	}
	if rightOnly, err = strconv.Atoi(fields[1]); err != nil {
		return 0, 0, fmt.Errorf("rev-list --left-right --count: %q: %w", fields[1], err)
	}
	return leftOnly, rightOnly, nil
}

// DiffNameOnlyRange lists the paths changed from merge-base(base, tip) to tip
// (`git diff --name-only -z <base>...<tip>` — git computes the merge base
// itself, so this is the GitHub "files changed" set in one invocation). -z
// keeps non-ASCII paths raw.
func (r *Repo) DiffNameOnlyRange(ctx context.Context, base, tip string) ([]string, error) {
	argv := gitcmd.New("diff").Arg("--name-only", "-z", base+"..."+tip).ToArgv()
	res, err := r.Runner.Run(ctx, "git diff --name-only (range)", argv)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range strings.Split(res.Stdout, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
