package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ModifiedIgnoringEOL returns, among paths, those whose working-tree content
// still differs from the index when carriage returns at line ends are ignored
// (`git diff --ignore-cr-at-eol`). A file whose ONLY difference is CRLF↔LF is
// omitted — `git status` flags it modified (its blob hash differs) but the
// content is unchanged.
//
// Detection uses `--numstat`, not `--name-only`: with core.autocrlf off,
// `--name-only` lists a file from the raw blob-pair difference and ignores the
// flag, whereas `--numstat` reports the post-ignore added/deleted line counts
// and simply omits a file with no real line changes. `-z` keeps non-ASCII
// paths intact (no core.quotepath escaping). Empty paths run no git command.
func (r *Repo) ModifiedIgnoringEOL(ctx context.Context, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	argv := gitcmd.New("diff").
		Arg("--ignore-cr-at-eol", "--numstat", "-z", "--").
		Arg(paths...).
		ToArgv()
	res, err := r.Runner.Run(ctx, "git diff (ignore-cr-at-eol)", argv)
	if err != nil {
		return nil, err
	}
	var out []string
	// Each NUL-terminated record is "<added>\t<deleted>\t<path>" for a normal
	// modification. (Renames have a different layout, but a status-'M' path is
	// never a rename, so the candidates here are always the simple form.)
	for _, rec := range strings.Split(res.Stdout, "\x00") {
		if rec == "" {
			continue
		}
		if parts := strings.SplitN(rec, "\t", 3); len(parts) == 3 && parts[2] != "" {
			out = append(out, parts[2])
		}
	}
	return out, nil
}
