package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// DiffNumstat returns `git diff --numstat -z` output for spec (raw, parse
// with ParseNumstat). -z keeps paths verbatim (no core.quotepath mangling)
// and makes rename records unambiguous.
func (r *Repo) DiffNumstat(ctx context.Context, spec model.DiffSpec) (string, error) {
	revs, err := r.diffRevs(ctx, spec)
	if err != nil {
		return "", err
	}
	b := gitcmd.New("diff").Arg("--numstat", "-z").
		ArgIf(spec.Cached, "--cached").
		Arg(revs...)
	if len(spec.Paths) > 0 {
		b.Arg("--").Arg(spec.Paths...)
	}
	res, err := r.Runner.Run(ctx, "git diff", b.ToArgv())
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// DiffPatch returns the full patch for spec, exactly as git prints it.
//
// The invocation is pinned to git's default a/b prefixes and to no colour,
// regardless of the user's diff.noprefix / diff.mnemonicPrefix / color.diff
// config: ParseDiffHunks (internal/domain) assumes the default "a/"/"b/"
// header prefixes and uncoloured output, so those user settings would
// otherwise make hunk parsing silently find nothing.
func (r *Repo) DiffPatch(ctx context.Context, spec model.DiffSpec) (string, error) {
	revs, err := r.diffRevs(ctx, spec)
	if err != nil {
		return "", err
	}
	b := gitcmd.New("diff").
		Config("diff.noprefix=false").
		Config("diff.mnemonicPrefix=false").
		Arg("--no-color").
		// A fixed context size is a forge's hunks (the send path): git's
		// default hunk shape, whatever the user's diff.interHunkContext /
		// diff.algorithm / diff.indentHeuristic say — and git's own text, never
		// a configured external diff or textconv filter.
		ArgIf(spec.Unified > 0, "-U"+strconv.Itoa(spec.Unified), "--inter-hunk-context=0",
			"--diff-algorithm=myers", "--indent-heuristic", "--no-ext-diff", "--no-textconv").
		ArgIf(spec.Cached, "--cached").
		Arg(revs...)
	if len(spec.Paths) > 0 {
		b.Arg("--").Arg(spec.Paths...)
	}
	res, err := r.Runner.Run(ctx, "git diff", b.ToArgv())
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// emptyTree is the empty tree's id in each object format: what a root
// commit's own change is diffed against (git knows it without storing it).
var emptyTree = map[string]string{
	"sha1":   "4b825dc642cb6eb9a060e54bf8d69288fbee4904",
	"sha256": "6ef19b41225c5369f1c104d45d8d85efa9b057b53b14b4b9b939dd74decc5321",
}

// diffRevs are spec's revision arguments: Rev as given, or for a root
// commit the empty tree and Rev (one more git call: the object format).
func (r *Repo) diffRevs(ctx context.Context, spec model.DiffSpec) ([]string, error) {
	switch {
	case spec.Rev == "":
		return nil, nil
	case !spec.Root:
		return []string{spec.Rev}, nil
	}
	format, err := r.ObjectFormat(ctx)
	if err != nil {
		return nil, err
	}
	tree, ok := emptyTree[format]
	if !ok {
		return nil, fmt.Errorf("unknown object format %q", format)
	}
	return []string{tree, spec.Rev}, nil
}

// ParseNumstat parses `--numstat -z` records: "A\tD\tpath\x00" ordinarily;
// a rename leaves the path field empty and appends old and new as the next
// two NUL fields; binary files carry "-" counts.
func ParseNumstat(out string) []model.DiffStat {
	fields := strings.Split(out, "\x00")
	var stats []model.DiffStat
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		parts := strings.SplitN(f, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		st := model.DiffStat{Path: parts[2]}
		if parts[0] == "-" {
			st.Binary = true
		} else {
			st.Added, _ = strconv.Atoi(parts[0])
			st.Deleted, _ = strconv.Atoi(parts[1])
		}
		if st.Path == "" { // rename: the next two fields are old, new
			if i+2 >= len(fields) || fields[i+1] == "" || fields[i+2] == "" {
				break
			}
			st.OldPath, st.Path = fields[i+1], fields[i+2]
			i += 2
		}
		stats = append(stats, st)
	}
	return stats
}
