package domain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/homeend/gigagit/internal/model"
)

// ErrComparePatchPair marks the one comparison ComparePatch cannot render: a
// pair of endpoints that is not one of git's four forward live forms. It is a
// gap in the PATCH lane only — CompareSets answers every pair — and it exists
// as a sentinel so a frontend refuses in its own words rather than forwarding
// livePairSpec's internal prose to a user.
//
// ComparePatchSets — the door a frontend holding file sets uses — catches it
// and renders the pair per member, so only a caller of ComparePatch itself
// (two endpoints by construction) can still see it.
var ErrComparePatchPair = errors.New("this pair cannot be rendered as a patch")

// CommitGoneError reports a commit-entry compare side whose sha no longer
// resolves and which has no frozen fallback (a bookmark stores no blobs).
// Frontends show a precise notice from it.
type CommitGoneError struct{ SHA string }

func (e *CommitGoneError) Error() string {
	sha := e.SHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return fmt.Sprintf("commit %s no longer exists", sha)
}

// ResolveCommitEntryEndpoint turns one side of a commit-entry comparison into
// a compare endpoint (hybrid semantics): the live sha while it resolves, the
// frozen tar (EndpointShelf) when a shelved side's sha is gone, and a
// CommitGoneError when a bookmark's sha is gone. sha must be the FULL sha the
// entry stores — CommitLookup serves only as the existence probe (it returns
// a short sha, which must not leak into the endpoint). Resolution is strictly
// per side, so mixed states compose: a shelf↔shelf pair with one gc'd sha
// becomes frozen↔live and lands in the shelf↔commit compare lane.
func (s *Service) ResolveCommitEntryEndpoint(ctx context.Context, sha, shelfID string) (model.Endpoint, error) {
	_, found, err := s.CommitLookup(ctx, sha)
	if err != nil {
		return model.Endpoint{}, err
	}
	if found {
		return model.CommitEndpoint(sha)
	}
	if shelfID != "" {
		return model.ShelfEndpoint(shelfID)
	}
	return model.Endpoint{}, &CommitGoneError{SHA: sha}
}

// shelfCompareFiles lists the files that differ when at least one side is a
// frozen shelf entry (left = older, right = newer, tree-diff conventions:
// only-in-left → D, only-in-right → A, differing bytes → M, identical →
// omitted).
//
// It is now a thin adapter: a shelf endpoint evaluates to a BOUNDED set (its
// members) and everything else to an unbounded one, so the two old shelf
// lanes are just two rows of the general algebra — shelf↔shelf is
// bounded × bounded, shelf↔commit is bounded × unbounded — and the
// shelfIsRight direction flag is gone: compareOne derives the same answer
// from which side actually holds the path. shelf↔commit stays scoped to the
// shelf's member paths for the same reason it always was: the frozen tar
// cannot speak for paths the shelved commit never changed, and that is also
// §3.5's "you can only ever scale DOWN".
//
// Deliberately NOT wrapped in one query(): each underlying read
// (ShelfCommitFiles, endpointHas, ShowFile, ResolveBytes) takes its own Read
// reservation, and nesting a gated read inside a held reservation can
// deadlock behind a queued writer.
func (s *Service) shelfCompareFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error) {
	l, err := s.EvalEndpoint(ctx, left)
	if err != nil {
		return nil, err
	}
	r, err := s.EvalEndpoint(ctx, right)
	if err != nil {
		return nil, err
	}
	return s.CompareSets(ctx, l, r)
}

// ComparePatch renders a unified diff for an endpoint pair. Live pairs go
// through git directly (one invocation); a pair involving a frozen shelf
// side is materialized per differing file into temp files and diffed with
// git diff --no-index, headers relabelled to a/<path> b/<path> (the MCP
// gg_compare_file precedent — git cannot see the tar). Per the spec's error
// handling rule ("unreadable/missing shelf blob → the store's error surfaces
// as-is"), a resolve failure on a side the file list says IS present
// propagates to the caller rather than being silently treated as absent —
// only the side an "A"/"D" status says is genuinely missing is left
// unresolved (empty bytes, no read attempted).
func (s *Service) ComparePatch(ctx context.Context, left, right model.Endpoint) (string, error) {
	if left.Kind() != model.EndpointShelf && right.Kind() != model.EndpointShelf {
		spec, err := livePairSpec(left, right)
		if err != nil {
			return "", err
		}
		return s.DiffPatch(ctx, spec)
	}
	l, err := s.EvalEndpoint(ctx, left)
	if err != nil {
		return "", err
	}
	r, err := s.EvalEndpoint(ctx, right)
	if err != nil {
		return "", err
	}
	return s.patchPerMember(ctx, l, r)
}

// isBinaryContent reports whether data looks binary — a NUL byte, or invalid
// UTF-8 — the same heuristic internal/mcp's gg_compare_file uses (isBinary).
// nil/empty data (an absent side) is never binary.
func isBinaryContent(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)
}

// livePairSpec maps a non-shelf endpoint pair onto the DiffSpec vocabulary:
// the four forward forms DiffTreeFiles supports (commit↔commit, commit→index,
// commit→worktree, index→worktree), which forwardLivePair also enumerates.
//
// NOTHING SCREENS THIS PAIR BEFORE IT ARRIVES. internal/cli used to hold a
// validComparePair predicate that turned an unsupported pair into a friendly
// message before the verb was reached, but it only ever covered the `gg
// compare` door and it has been deleted outright — CompareSets is total over
// the bounded/unbounded 2×2, so there is no invalid pair to screen for any
// more. It used to be the only thing standing between a stray pair and a
// `default:` arm here that returned an empty DiffSpec — which git reads as
// "index → working tree". A link now hands ComparePatch whatever endpoint it
// produced, so a PAIR or a REF arriving here would have rendered a diff of
// something else entirely with no error at all. An unhandled pair is a refusal
// now, in the same wording DiffTreeFiles uses for its own.
//
// ComparePatch is therefore NOT total the way CompareSets is: it still takes
// two endpoints, so a reversed live pair is refused here rather than asked
// forward and inverted (see CompareSets' unbounded × unbounded arm), and a
// bounded × bounded pair renders the endpoints' whole diff rather than the
// projection. ComparePatchSets is the total door: it sends both of those to
// the per-member lane (patchPerMember).
//
// The refusal wraps ErrComparePatchPair so a frontend can recognise it without
// matching on prose and say so in its OWN words: this message names a Go
// function and two raw enum ordinals, and a user's terminal is the wrong place
// for either.
func livePairSpec(left, right model.Endpoint) (model.DiffSpec, error) {
	switch {
	case left.Kind() == model.EndpointCommit && right.Kind() == model.EndpointCommit:
		return model.DiffSpec{Rev: left.Hash() + ".." + right.Hash()}, nil
	case left.Kind() == model.EndpointCommit && right.Kind() == model.EndpointIndex:
		return model.DiffSpec{Cached: true, Rev: left.Hash()}, nil
	case left.Kind() == model.EndpointCommit && right.Kind() == model.EndpointWorkTree:
		return model.DiffSpec{Rev: left.Hash()}, nil
	case left.Kind() == model.EndpointIndex && right.Kind() == model.EndpointWorkTree:
		return model.DiffSpec{}, nil // bare `git diff` is already index → worktree
	}
	return model.DiffSpec{}, fmt.Errorf("%w: livePairSpec: unsupported endpoint pair %d → %d", ErrComparePatchPair, left.Kind(), right.Kind())
}

// RelabelNoIndexDiff strips the temp-path noise from git diff --no-index
// output: drops the "diff --git"/"index" header lines and rewrites ---/+++
// to the given display labels. Header rewriting stops at the first @@ hunk
// line so body lines that merely look like headers (e.g. a removed SQL
// comment "-- foo" renders as "--- foo") are never touched. Shared by the
// MCP gg_compare_file tool and ComparePatch's frozen lane.
func RelabelNoIndexDiff(diff, leftDisplay, rightDisplay string) string {
	lines := strings.Split(diff, "\n")
	out := make([]string, 0, len(lines))
	inHeader := true
	for _, ln := range lines {
		if inHeader {
			switch {
			case strings.HasPrefix(ln, "@@"):
				inHeader = false
			case strings.HasPrefix(ln, "diff --git "), strings.HasPrefix(ln, "index "):
				continue
			case strings.HasPrefix(ln, "--- "):
				out = append(out, "--- "+leftDisplay)
				continue
			case strings.HasPrefix(ln, "+++ "):
				out = append(out, "+++ "+rightDisplay)
				continue
			}
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}
