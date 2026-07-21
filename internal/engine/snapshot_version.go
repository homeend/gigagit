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
}

// snapshotNow is a test seam for the snapshot timestamp.
var snapshotNow = time.Now

// snapshotBranchTip records branch's current tip as a hidden version ref
// (refs/gg/versions/<branch>/<ts>-<opToken>) and prunes expired versions of
// that branch. BEST-EFFORT by contract: any failure emits a progress note and
// returns — recording must never block or fail the real operation.
func snapshotBranchTip(ctx context.Context, deps OpDeps, branch, opToken string) {
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
	deps.emit(ctx, Progress{Step: "recording branch version", Detail: branch})
	if err := deps.Repo.UpdateRef(ctx, ref, sha); err != nil {
		deps.emit(ctx, Progressf("recording branch version", "skipped: %s", err.Error()))
		return
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
