package domain

import (
	"context"
	"fmt"
	"strings"
)

// stashShapeInfo names the parts of a `-u` stash a change-set needs.
type stashShapeInfo struct {
	Untracked string // the third parent: a parentless commit holding the untracked files
}

// stashShape is the SET rule of spec §3.4 and it is STRUCTURAL ONLY: b has
// exactly three parents, a is the first, and the third is a ROOT commit.
//
// The root test is load-bearing. An octopus merge also has three parents, and
// on a monorepo its third parent's tree is every file in the repository — all
// of which would enter the change-set as additions. A stash's untracked commit
// is always parentless; a merged-in root is not something a real history has.
//
// It never reads a subject line: this rule changes what a comparison REPORTS,
// so it must not depend on text a user can edit (`git stash push -m`).
// looksLikeAStash is the loose, description-only sibling — keep them apart.
func (s *Service) stashShape(ctx context.Context, a, b string) (stashShapeInfo, bool, error) {
	parents, err := s.commitParents(ctx, b)
	if err != nil {
		return stashShapeInfo{}, false, err
	}
	if len(parents) != 3 || parents[0] != a {
		return stashShapeInfo{}, false, nil
	}
	grand, err := s.commitParents(ctx, parents[2])
	if err != nil {
		return stashShapeInfo{}, false, err
	}
	if len(grand) != 0 {
		return stashShapeInfo{}, false, nil
	}
	return stashShapeInfo{Untracked: parents[2]}, true, nil
}

// looksLikeAStash is the DESCRIPTION rule: a is b's first parent, b has two or
// three parents, and b's subject reads like git's own stash subjects ("WIP on
// <branch>: …" or "On <branch>: …"). An ordinary merge passes the parent test,
// so the subject is what tells them apart — good enough for wording a history
// row, where a wrong guess costs one mislabelled line, and NOT good enough to
// change a comparison (that is stashShape's job). Best-effort: any error is
// "no".
func (s *Service) looksLikeAStash(ctx context.Context, a, b string) (subject string, ok bool) {
	parents, err := s.commitParents(ctx, b)
	if err != nil || (len(parents) != 2 && len(parents) != 3) || parents[0] != a {
		return "", false
	}
	line, found, err := s.CommitLookup(ctx, b)
	if err != nil || !found {
		return "", false
	}
	if !strings.HasPrefix(line.Subject, "WIP on ") && !strings.HasPrefix(line.Subject, "On ") {
		return "", false
	}
	return line.Subject, true
}

// commitParents is git.CommitParents under a Read reservation. Keyed on the
// rev as given: every caller here passes a resolved sha, whose parents never
// change.
func (s *Service) commitParents(ctx context.Context, rev string) ([]string, error) {
	return query(ctx, s, "commit-parents:"+rev, func(ctx context.Context) ([]string, error) {
		return s.repo.CommitParents(ctx, rev)
	})
}

// StashPair resolves a POSITIONAL stash ref (stash@{N}) to the pair a link may
// carry: the stash commit and its first parent, both as full shas. The ref is
// an input only — it must never reach a link, because pushing or dropping any
// stash renumbers every N (spec §3.4 rule 1).
func (s *Service) StashPair(ctx context.Context, ref string) (parent, sha string, err error) {
	sha, err = s.StashCommit(ctx, ref)
	if err != nil {
		return "", "", err
	}
	// Parents are asked of the SHA, never of the ref: the read is coalesced by
	// key, and a key holding stash@{N} would name a different stash next time.
	parents, err := s.commitParents(ctx, sha)
	if err != nil {
		return "", "", err
	}
	if len(parents) == 0 {
		return "", "", fmt.Errorf("%s is not a stash: it has no parent", ref)
	}
	return parents[0], sha, nil
}
