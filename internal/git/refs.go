package git

import (
	"context"
	"os"
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

// VersionRefs lists version snapshots under prefix in ONE invocation, unwrapping
// each synthetic commit: Hash is the snapshotted tip (the first parent), and the
// endpoints come from the single Gg-Meta trailer.
//
// A ref predating the synthetic-commit format (created by UpdateRef straight
// at the tip — no Gg-Meta trailer at all, so the trailers atom renders "") has
// no wrapper to unwrap: Hash falls back to %(objectname), the ref's own
// target, rather than its first parent (which for a legacy ref is one commit
// too far back, or absent entirely on a root commit). Whether to unwrap is
// decided by the trailer's PRESENCE, not by ParseVersionMeta succeeding: a
// synthetic commit whose trailer fails to parse is still a wrapper (Hash must
// still come from the first parent) — it just contributes no endpoints.
//
// The format string must contain exactly ONE %(trailers:key=…) atom — see
// metaTrailerKey in versionrecord.go for why.
func (r *Repo) VersionRefs(ctx context.Context, prefix string) ([]model.BranchVersion, error) {
	const format = "%(refname)%00%(objectname)%00%(parent)%00%(subject)%00" +
		"%(trailers:key=" + metaTrailerKey + ",valueonly,separator=%x20)"
	argv := gitcmd.New("for-each-ref").Arg("--format="+format, prefix).ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref (gg)", argv)
	if err != nil {
		return nil, err
	}
	var out []model.BranchVersion
	for _, ln := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		parts := strings.Split(ln, "\x00")
		if len(parts) < 5 || parts[0] == "" {
			continue
		}
		_, op, unix, ok := ParseVersionRef(parts[0])
		if !ok {
			continue
		}
		bv := model.BranchVersion{Ref: parts[0], Hash: parts[1], Subject: parts[3], Op: op, Unix: unix}
		if parts[4] != "" {
			// A trailer is present: this is the synthetic-commit format.
			// Unwrap to the snapshotted tip, the first parent (%(parent)
			// lists every parent space-separated) — even if the trailer
			// itself fails to parse, in which case it contributes no
			// endpoints but the commit is still a wrapper.
			if ps := strings.Fields(parts[2]); len(ps) > 0 {
				bv.Hash = ps[0]
			}
			if m, ok := ParseVersionMeta(parts[4]); ok {
				bv.Ours, bv.Other, bv.Base = m.Ours, m.Other, m.Base
				bv.Source, bv.Target = m.Source, m.Target
				if m.Op != "" {
					bv.Op = m.Op
				}
			}
		}
		out = append(out, bv)
	}

	if len(out) > 0 {
		distinct := map[string]struct{}{}
		var shas []string
		for _, bv := range out {
			if bv.Hash == "" {
				continue
			}
			if _, seen := distinct[bv.Hash]; !seen {
				distinct[bv.Hash] = struct{}{}
				shas = append(shas, bv.Hash)
			}
		}
		if subjects, err := r.subjectsOf(ctx, shas); err == nil {
			for i, bv := range out {
				if s, ok := subjects[bv.Hash]; ok {
					out[i].Subject = s
				}
			}
		}
	}

	return out, nil
}

// subjectsOf batch-resolves each sha's own commit subject in ONE invocation
// (git log --no-walk --format=%H%x00%s <sha>… prints one line per input
// commit). VersionRefs uses it to overwrite the synthetic snapshot commit's
// subject with the snapshotted tip's real subject.
func (r *Repo) subjectsOf(ctx context.Context, shas []string) (map[string]string, error) {
	if len(shas) == 0 {
		return nil, nil
	}
	b := gitcmd.New("log").Arg("--no-walk", "--format=%H%x00%s")
	for _, sha := range shas {
		b = b.Arg(sha)
	}
	res, err := r.Runner.Run(ctx, "git log (subjects)", b.ToArgv())
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, ln := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		h, subj, ok := strings.Cut(ln, "\x00")
		if ok {
			out[h] = subj
		}
	}
	return out, nil
}

// EmptyTree writes (idempotently) the empty tree object and returns its id. It
// exists so marker refs have a target that pins nothing meaningful: the format
// number lives in the REF NAME, and the object is only there because
// update-ref requires one.
//
// The id is a constant per hash algorithm, but it is not hardcoded — a
// sha256 repository has a different one. `hash-object` is given an empty
// temporary FILE rather than /dev/null or --stdin: the Runner has no stdin,
// and /dev/null is not portable to Windows.
func (r *Repo) EmptyTree(ctx context.Context) (string, error) {
	f, err := os.CreateTemp("", "gg-empty-tree")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(name) }()

	argv := gitcmd.New("hash-object").Arg("-t", "tree", "-w", name).ToArgv()
	res, err := r.Runner.Run(ctx, "git hash-object", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
