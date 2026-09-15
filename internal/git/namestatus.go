package git

import (
	"context"
	"strings"

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
	return parseNameStatusEntries(res.Stdout), nil
}

// parseNameStatusEntries walks -z name-status output. With --no-renames a
// rename/copy pair (status, old path, new path) cannot occur, but the parser
// still recognizes the R/C shape defensively: it consumes all three fields as
// one unit and skips the entry (changeset.Entry's status contract is A/M/D/T,
// not R/C) rather than either shifting the pairing of every entry that
// follows it (a misparse) or panicking on a short tail.
func parseNameStatusEntries(stdout string) []changeset.Entry {
	toks := strings.Split(strings.TrimRight(stdout, "\x00"), "\x00")
	var out []changeset.Entry
	for i := 0; i < len(toks); {
		status := toks[i]
		i++
		if status == "" {
			continue
		}
		letter := status[0]
		if letter == 'R' || letter == 'C' {
			if i+1 >= len(toks) { // need old + new path; malformed tail → stop
				break
			}
			i += 2 // consume the pair whole; skip, don't emit an R/C entry
			continue
		}
		if i >= len(toks) { // need a path; malformed tail → stop
			break
		}
		out = append(out, changeset.Entry{Status: letter, Path: toks[i]})
		i++
	}
	return out
}
