package git

import (
	"context"

	"github.com/homeend/gigagit/internal/changeset"
	"github.com/homeend/gigagit/internal/gitcmd"
)

// DiffNameStatus lists the paths that differ between two commit-ish values
// with their status letter, one git invocation.
//
// --no-renames is not a preference: a later comparison diffs two of these
// listings as sets, and with rename detection on a D+A pair on one side can be
// reported as a single R on the other, manufacturing differences that are not
// there. -z keeps paths with spaces or newlines intact and NUL-separates the
// fields instead of relying on newlines.
func (r *Repo) DiffNameStatus(ctx context.Context, a, b string) ([]changeset.Entry, error) {
	argv := gitcmd.New("diff").Arg("--name-status", "--no-renames", "-z", a, b).ToArgv()
	res, err := r.Runner.Run(ctx, "git diff --name-status", argv)
	if err != nil {
		return nil, err
	}
	files := ParseNameStatus([]byte(res.Stdout))
	out := make([]changeset.Entry, 0, len(files))
	for _, f := range files {
		// --no-renames means git never emits R/C here in practice; skip any
		// that somehow arrived rather than mis-mapping them, since
		// changeset.Entry has no OldPath field to carry a rename/copy.
		if f.Status == "R" || f.Status == "C" {
			continue
		}
		out = append(out, changeset.Entry{Status: f.Status[0], Path: f.Path})
	}
	return out, nil
}
