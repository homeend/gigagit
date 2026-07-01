package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// reflogFmt joins fields with NUL and records with newline. %gs (the reflog
// subject) is single-line, so newline record splitting is safe. Under
// --date=relative the %gd selector renders as "HEAD@{2 minutes ago}", which is
// how we recover the entry's relative time — git has no standalone reflog
// relative-time placeholder (%gr does not exist; it prints literally).
const reflogFmt = "%H%x00%h%x00%gd%x00%gs"

// ReflogEntries returns up to limit HEAD reflog entries, newest first. A repo
// with no reflog yields an empty slice (not an error).
func (r *Repo) ReflogEntries(ctx context.Context, limit int) ([]model.ReflogEntry, error) {
	// --date=relative turns %gd into the human-readable "HEAD@{2 minutes ago}"
	// form; we split the relative time back out and rebuild the addressable
	// numeric selector (HEAD@{N}) from the row index.
	b := gitcmd.New("reflog").Arg("--date=relative", "--format="+reflogFmt)
	if limit > 0 {
		b = b.Arg("-n", strconv.Itoa(limit))
	}
	res, err := r.Runner.Run(ctx, "git reflog", b.ToArgv())
	if err != nil {
		return nil, err
	}
	var out []model.ReflogEntry
	for _, line := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 4 {
			continue
		}
		out = append(out, model.ReflogEntry{
			Hash:      f[0],
			ShortHash: f[1],
			Selector:  "HEAD@{" + strconv.Itoa(len(out)) + "}",
			Subject:   f[3],
			Rel:       reflogRelTime(f[2]),
		})
	}
	return out, nil
}

// reflogRelTime extracts the bare relative time from a --date=relative selector
// like "HEAD@{2 minutes ago}" → "2 minutes ago". Anything unexpected is
// returned unchanged.
func reflogRelTime(selector string) string {
	i := strings.IndexByte(selector, '{')
	if i < 0 {
		return selector
	}
	return strings.TrimSuffix(selector[i+1:], "}")
}

// LastReflogSubject returns the subject of the most recent HEAD reflog entry,
// e.g. "commit: add foo" or "checkout: moving from main to dev". Returns "" if
// there is no reflog.
func (r *Repo) LastReflogSubject(ctx context.Context) (string, error) {
	argv := gitcmd.New("reflog").Arg("-1", "--format=%gs").ToArgv()
	res, err := r.Runner.Run(ctx, "git reflog", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// ResetSoft moves the current branch to ref, leaving the index and working tree
// unchanged (git reset --soft). The undone commit's changes remain staged.
func (r *Repo) ResetSoft(ctx context.Context, ref string) error {
	argv := gitcmd.New("reset").Arg("--soft", ref).ToArgv()
	_, err := r.Runner.Run(ctx, "git reset --soft", argv)
	return err
}

// Reset moves the current branch to ref with the given mode:
//   - "soft":  index + working tree kept (the diff since ref stays staged)
//   - "mixed": index reset, working tree kept (the diff stays unstaged)
//   - "hard":  index + working tree reset (uncommitted TRACKED changes discarded;
//     untracked files survive)
func (r *Repo) Reset(ctx context.Context, mode, ref string) error {
	argv := gitcmd.New("reset").Arg("--"+mode, ref).ToArgv()
	_, err := r.Runner.Run(ctx, "git reset --"+mode, argv)
	return err
}
