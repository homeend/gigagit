package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// StatusPaths lists the paths under the pathspec that git status reports —
// staged, unstaged, deleted, untracked, both sides of a rename: "this file
// on disk is not the committed one". Limited by pathspec so the cost is the
// question's, not the repository's (a monorepo's full status takes
// seconds). An empty paths list returns nil without invoking git.
func (r *Repo) StatusPaths(ctx context.Context, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	argv := gitcmd.New("status").Arg("--porcelain=v2", "-z", "--untracked-files=all", "--").Arg(literalPathspec(paths)...).ToArgv()
	res, err := r.Runner.RunEnv(ctx, "git status (paths)", argv, readOnlyStatusEnv)
	if err != nil {
		return nil, err
	}
	st, err := ParseStatusV2([]byte(res.Stdout))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range st.Files {
		out = append(out, f.Path)
		if f.OrigPath != "" {
			out = append(out, f.OrigPath)
		}
	}
	return out, nil
}

// DiffNamesBetween lists the paths under the pathspec whose content differs
// between commits a and b (`git diff --name-only -z a b -- <paths>`). An
// empty paths list returns nil without invoking git.
func (r *Repo) DiffNamesBetween(ctx context.Context, a, b string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	argv := gitcmd.New("diff").Arg("--name-only", "-z", "--no-renames", a, b, "--").Arg(literalPathspec(paths)...).ToArgv()
	res, err := r.Runner.Run(ctx, "git diff --name-only (paths)", argv)
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
