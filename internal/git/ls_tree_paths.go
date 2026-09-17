package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// TreePaths returns the subset of paths that commit's tree actually contains.
//
// It is TreeFiles' scaled-down twin: same `ls-tree -r --name-only -z`, but
// limited by pathspec, so the cost is the size of the QUESTION rather than the
// size of the repository. That distinction is the whole point — TreeFiles on a
// ~1M-path head is ~100MB of stdout, and asking it whether three files exist is
// the "you can only ever scale DOWN" rule inverted. TreeFiles stays for the
// callers that genuinely want the whole tree.
//
// An empty paths list returns nil WITHOUT invoking git: `ls-tree … --` with no
// pathspec lists the entire tree, which is the opposite of what a caller
// passing no paths means.
func (r *Repo) TreePaths(ctx context.Context, commit string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	argv := gitcmd.New("ls-tree").Arg("-r", "--name-only", "-z", commit).Arg("--").Arg(paths...).ToArgv()
	res, err := r.Runner.Run(ctx, "git ls-tree (tree paths)", argv)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range strings.Split(strings.TrimRight(res.Stdout, "\x00"), "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
