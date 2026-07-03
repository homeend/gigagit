package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// DiffNumstat returns `git diff --numstat -z` output for spec (raw, parse
// with ParseNumstat). -z keeps paths verbatim (no core.quotepath mangling)
// and makes rename records unambiguous.
func (r *Repo) DiffNumstat(ctx context.Context, spec model.DiffSpec) (string, error) {
	b := gitcmd.New("diff").Arg("--numstat", "-z").
		ArgIf(spec.Cached, "--cached").
		ArgIf(spec.Rev != "", spec.Rev)
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
func (r *Repo) DiffPatch(ctx context.Context, spec model.DiffSpec) (string, error) {
	b := gitcmd.New("diff").
		ArgIf(spec.Cached, "--cached").
		ArgIf(spec.Rev != "", spec.Rev)
	if len(spec.Paths) > 0 {
		b.Arg("--").Arg(spec.Paths...)
	}
	res, err := r.Runner.Run(ctx, "git diff", b.ToArgv())
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
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
			if i+2 >= len(fields) {
				break
			}
			st.OldPath, st.Path = fields[i+1], fields[i+2]
			i += 2
		}
		stats = append(stats, st)
	}
	return stats
}
