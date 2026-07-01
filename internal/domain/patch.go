package domain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// ErrMergeCommitPatch is returned when a patch export targets a merge commit.
// git format-patch -1 does not error on a merge — it silently skips the merge
// and emits a DIFFERENT commit's patch — so callers must be refused up front.
var ErrMergeCommitPatch = errors.New("cannot export a merge commit as a patch")

// shortSHA abbreviates an object id to 7 chars for human-facing file names.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// CommitPatch returns the git am-able patch for sha's whole change set plus the
// default file name (<shortsha>.patch). Refuses a merge commit (ErrMergeCommitPatch).
func (s *Service) CommitPatch(ctx context.Context, sha string) ([]byte, string, error) {
	if err := s.refuseMerge(ctx, sha); err != nil {
		return nil, "", err
	}
	data, err := query(ctx, s, "commitpatch:"+sha, func(ctx context.Context) ([]byte, error) {
		return s.repo.FormatPatch(ctx, sha)
	})
	if err != nil {
		return nil, "", err
	}
	return data, shortSHA(sha) + ".patch", nil
}

// FilePatch returns the git am-able patch for a single file's change within sha
// plus the default file name (<shortsha>-<basename>.patch). Refuses a merge.
func (s *Service) FilePatch(ctx context.Context, sha, path string) ([]byte, string, error) {
	if err := s.refuseMerge(ctx, sha); err != nil {
		return nil, "", err
	}
	data, err := query(ctx, s, "filepatch:"+sha+":"+path, func(ctx context.Context) ([]byte, error) {
		return s.repo.FormatPatch(ctx, sha, path)
	})
	if err != nil {
		return nil, "", err
	}
	return data, shortSHA(sha) + "-" + filepath.Base(path) + ".patch", nil
}

// refuseMerge returns ErrMergeCommitPatch when sha has more than one parent.
// The parent-count read uses queryQuiet so an expected merge refusal (validation,
// not an operational failure) never lands in errors.log / the Session-errors viewer.
func (s *Service) refuseMerge(ctx context.Context, sha string) error {
	n, err := queryQuiet(ctx, s, "parentcount:"+sha, func(ctx context.Context) (int, error) {
		return s.repo.ParentCount(ctx, sha)
	})
	if err != nil {
		return err
	}
	if n > 1 {
		return ErrMergeCommitPatch
	}
	return nil
}

// ExportDefaultDir is the default directory a patch export writes into: the
// parent of the MAIN worktree root (e.g. /a/x/repo -> /a/x), stable even from a
// linked worktree. Mirrors TempExportBase's main-worktree anchor.
func (s *Service) ExportDefaultDir(ctx context.Context) (string, error) {
	wts, err := s.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	if len(wts) == 0 || wts[0].Path == "" {
		return "", fmt.Errorf("export: no main worktree")
	}
	return filepath.Dir(filepath.Clean(wts[0].Path)), nil
}
