package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// UpdateRef points ref at sha (git update-ref), creating it if missing.
func (r *Repo) UpdateRef(ctx context.Context, ref, sha string) error {
	_, err := r.Runner.Run(ctx, "git update-ref", gitcmd.New("update-ref").Arg(ref, sha).ToArgv())
	return err
}

// DeleteRef removes ref (git update-ref -d).
func (r *Repo) DeleteRef(ctx context.Context, ref string) error {
	_, err := r.Runner.Run(ctx, "git update-ref", gitcmd.New("update-ref").Arg("-d", ref).ToArgv())
	return err
}

// ForEachRef lists refs under a slash-boundary prefix (no glob), one
// invocation, with target sha and commit subject. NUL separators survive any
// subject content (the Branches verb precedent).
func (r *Repo) ForEachRef(ctx context.Context, prefix string) ([]model.RefInfo, error) {
	argv := gitcmd.New("for-each-ref").Arg("--format=%(refname)%00%(objectname)%00%(subject)", prefix).ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref (gg)", argv)
	if err != nil {
		return nil, err
	}
	var out []model.RefInfo
	for _, ln := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		ref, rest, ok := strings.Cut(ln, "\x00")
		if !ok || ref == "" {
			continue
		}
		hash, subject, _ := strings.Cut(rest, "\x00")
		out = append(out, model.RefInfo{Ref: ref, Hash: hash, Subject: subject})
	}
	return out, nil
}
