package engine

import (
	"context"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// VersionsPolicy governs pre-operation branch-version snapshots. The zero
// value DISABLES snapshots so operations run with bare OpDeps (tests, direct
// callers) stay byte-identical; domain.Execute injects the real policy
// (default: enabled, 90 days). MaxAgeDays <= 0 means never prune.
type VersionsPolicy struct {
	Enabled    bool
	MaxAgeDays int
	// Format is the store-format number the WRITER stamps (see
	// StampStoreFormat below). It is INJECTED by domain (domain.VersionsFormat
	// is the declared truth and internal/engine must not import
	// internal/domain), so a later spec bumping the format cannot drift from
	// the number the preflight resolver checks. Zero means "unknown": the
	// snapshot is still recorded, but no marker is stamped.
	Format int
}

// snapshotNow is a test seam for the snapshot timestamp.
var snapshotNow = time.Now

// tipOf resolves branch's current local tip for a version snapshot's
// Ours/Other endpoint. Best-effort: an error (unborn/unknown branch) comes
// back as "", which snapshotBranchTip's ours/other guard turns into "record
// no endpoints" rather than a failure.
func tipOf(ctx context.Context, deps OpDeps, branch string) string {
	sha, _ := deps.Repo.RevParse(ctx, "refs/heads/"+branch)
	return sha
}

// snapshotBranchTip records branch's current tip as a hidden version ref
// (refs/gg/versions/<branch>/<ts>-<opToken>) and prunes expired versions of
// that branch. ours/other are the two-branch endpoints a preview needs later
// (the contribution and what it lands on/against) — both empty for a
// one-branch op (amend, reset, undo-commit, delete-branch, restore), which
// records no preview.
//
// This is the short form for the common case where the snapshotted branch
// IS the Source side and other IS the Target name/ref as given (every
// two-branch call site except merge — see snapshotBranchTipNamed). BEST-
// EFFORT by contract: any failure emits a progress note and returns —
// recording must never block or fail the real operation.
func snapshotBranchTip(ctx context.Context, deps OpDeps, branch, opToken, ours, other string) {
	snapshotBranchTipNamed(ctx, deps, branch, opToken, ours, other, branch, other)
}

// snapshotBranchTipNamed is snapshotBranchTip with explicit Source/Target
// names. It exists because branch (the ref actually being snapshotted) and
// other (the second merge-base endpoint) don't always line up with the
// Source/Target a PR-style preview wants to label: for merge, the
// snapshotted branch is the merge TARGET (main), but Ours/Source is the
// merge SOURCE (feat) — a name that never otherwise reaches this function,
// since ours only carries feat's resolved tip sha, not its name. Every
// other two-branch op's Source/Target already coincide with (branch, other)
// and goes through the snapshotBranchTip short form above.
func snapshotBranchTipNamed(ctx context.Context, deps OpDeps, branch, opToken, ours, other, source, target string) {
	if !deps.Versions.Enabled || branch == "" {
		return
	}
	sha, err := deps.Repo.RevParse(ctx, "refs/heads/"+branch)
	if err != nil || sha == "" {
		return // unborn or unknown branch: nothing to record
	}
	ts := snapshotNow().Unix()
	ref := git.VersionRef(branch, opToken, ts)
	// Same-second, same-op collision: bump the timestamp until free.
	existing := map[string]bool{}
	infos, err := deps.Repo.ForEachRef(ctx, "refs/gg/versions/"+branch)
	if err == nil {
		for _, i := range infos {
			existing[i.Ref] = true
		}
	}
	for existing[ref] {
		ts++
		ref = git.VersionRef(branch, opToken, ts)
	}

	meta := git.VersionMeta{Op: opToken}
	if ours != "" && other != "" {
		// Other is RESOLVED to a sha here, not stored as the name the call
		// site passed. Callers hand us whatever names the second endpoint —
		// `op.Onto` for rebase (which can be a revision like HEAD~3),
		// "<remote>/<branch>" for pull — and a NAME re-resolves at diff time,
		// long after the op moved it: `gg rebase HEAD~3` would later compare
		// against the POST-rebase HEAD, a wholly unrelated commit, and a
		// recorded origin/<b> would slide forward on the next fetch. A frozen
		// record must freeze both of its endpoints. One extra rev-parse is the
		// price; Target keeps the name exactly as given, for labelling.
		//
		// Base cannot be recomputed after the op: once the branches have
		// merged, merge-base(target, source) returns the source tip rather
		// than the fork point. Record it now or lose it. Source/Target ride
		// along with Ours/Other/Base as one unit — git.ParseVersionMeta only
		// accepts a record at exactly 1 or 6 fields, so a merge-base failure
		// (or either endpoint missing, or Other unresolvable) must leave EVERY
		// endpoint field empty, never a partial 5-field record that silently
		// loses its preview.
		otherSha, oerr := deps.Repo.RevParse(ctx, other)
		if oerr == nil && otherSha != "" {
			if base, berr := deps.Repo.MergeBase(ctx, ours, otherSha); berr == nil && base != "" {
				meta.Ours, meta.Other, meta.Base = ours, otherSha, base
				meta.Source, meta.Target = source, target
			}
		}
	}

	deps.emit(ctx, Progress{Step: "recording branch version", Detail: branch})
	syn, err := deps.Repo.WriteVersionSnapshot(ctx, sha, meta, ts)
	if err != nil {
		deps.emit(ctx, Progressf("recording branch version", "skipped: %s", err.Error()))
		return
	}
	if err := deps.Repo.UpdateRef(ctx, ref, syn); err != nil {
		deps.emit(ctx, Progressf("recording branch version", "skipped: %s", err.Error()))
		return
	}
	// The store's WRITER stamps the format marker — never startup. Keeps every
	// `gg` invocation free of a ref write and removes the compare-and-swap race
	// between concurrently starting processes. Best-effort like the snapshot
	// itself: a stamp failure must not fail the real operation.
	if deps.Versions.Format > 0 {
		_ = deps.Repo.StampStoreFormat(ctx, "versions", deps.Versions.Format)
	}
	pruneBranchVersions(ctx, deps, branch, infos)
}

// pruneBranchVersions deletes this branch's version refs older than the
// policy age. infos is the pre-snapshot listing (the fresh ref is never
// expired). Best-effort: delete errors are ignored.
func pruneBranchVersions(ctx context.Context, deps OpDeps, branch string, infos []model.RefInfo) {
	if deps.Versions.MaxAgeDays <= 0 {
		return
	}
	cutoff := snapshotNow().AddDate(0, 0, -deps.Versions.MaxAgeDays).Unix()
	for _, info := range infos {
		b, _, ts, ok := git.ParseVersionRef(info.Ref)
		if !ok || b != branch || ts >= cutoff {
			continue
		}
		_ = deps.Repo.DeleteRef(ctx, info.Ref)
	}
}
