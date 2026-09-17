package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// LsFiles returns tracked files (paths relative to the working-tree root).
// NUL-delimited (-z) so paths with spaces or non-ASCII bytes come through raw.
//
// With no paths it lists every tracked file — the file finder, the file-path
// popup and web deep search all want that whole list. With paths it is limited
// to that PATHSPEC and answers the narrower question "which of these does the
// index hold", which is how domain's endpointHas probes an index endpoint
// without enumerating a ~100GB monorepo. `--` separates the pathspec so a path
// that looks like an option cannot be read as one, and each element is wrapped
// in `:(literal)` by literalPathspec — see there for what raw paths get wrong.
//
// The listing is the INDEX's, not the disk's: a tracked file the user removed
// from disk is still reported here, and `ls-files --deleted` does NOT reliably
// find it (a skip-worktree entry is invisible to it). Anything that needs to
// know what is actually ON DISK must stat — see WorktreeFilesPresent.
func (r *Repo) LsFiles(ctx context.Context, paths ...string) ([]string, error) {
	b := gitcmd.New("ls-files").Arg("-z")
	if len(paths) > 0 {
		b = b.Arg("--").Arg(literalPathspec(paths)...)
	}
	res, err := r.Runner.Run(ctx, "git ls-files", b.ToArgv())
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
