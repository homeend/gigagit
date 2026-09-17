package domain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/homeend/gigagit/internal/model"
)

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
	files, err := s.shelfCompareFiles(ctx, left, right)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "gg-compare-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	var b strings.Builder
	for i, f := range files {
		var lb, rb []byte
		switch f.Status {
		case "A": // left genuinely absent; only the right side is read
			rb, err = s.ResolveBytes(ctx, right.FileRef(f.Path))
		case "D": // right genuinely absent; only the left side is read
			lb, err = s.ResolveBytes(ctx, left.FileRef(f.Path))
		default: // "M": both sides are present — resolve both, any error propagates
			lb, err = s.ResolveBytes(ctx, left.FileRef(f.Path))
			if err == nil {
				rb, err = s.ResolveBytes(ctx, right.FileRef(f.Path))
			}
		}
		if err != nil {
			return "", err
		}
		if isBinaryContent(lb) || isBinaryContent(rb) {
			// git diff --no-index would print the temp paths on this line
			// (no @@ hunk to flip RelabelNoIndexDiff's header latch), so a
			// binary pair is rendered directly instead of ever being diffed.
			fmt.Fprintf(&b, "Binary files a/%s and b/%s differ\n", f.Path, f.Path)
			continue
		}
		lp := filepath.Join(tmp, fmt.Sprintf("l%d", i))
		rp := filepath.Join(tmp, fmt.Sprintf("r%d", i))
		if err := os.WriteFile(lp, lb, 0o600); err != nil {
			return "", err
		}
		if err := os.WriteFile(rp, rb, 0o600); err != nil {
			return "", err
		}
		diff, err := s.DiffNoIndex(ctx, lp, rp)
		if err != nil {
			return "", err
		}
		b.WriteString(RelabelNoIndexDiff(diff, "a/"+f.Path, "b/"+f.Path))
	}
	return b.String(), nil
}

// isBinaryContent reports whether data looks binary — a NUL byte, or invalid
// UTF-8 — the same heuristic internal/mcp's gg_compare_file uses (isBinary).
// nil/empty data (an absent side) is never binary.
func isBinaryContent(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)
}

// livePairSpec maps a non-shelf endpoint pair onto the DiffSpec vocabulary:
// the four forward forms the compare surfaces produce (commit↔commit,
// commit→index, commit→worktree, index→worktree).
//
// It used to end in a `default:` that meant "index → working tree", which was
// safe only while those four were the only pairs that could reach it. They are
// not any more: a link now hands ComparePatch whatever endpoint it produced,
// so a PAIR or a REF arriving here would have rendered a diff of something
// else entirely, with no error at all. An unhandled pair is now a refusal.
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
	return model.DiffSpec{}, fmt.Errorf("livePairSpec: unsupported endpoint pair %d → %d", left.Kind(), right.Kind())
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
