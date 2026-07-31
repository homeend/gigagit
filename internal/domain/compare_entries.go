package domain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
		return model.Endpoint{Kind: model.EndpointCommit, Hash: sha}, nil
	}
	if shelfID != "" {
		return model.Endpoint{Kind: model.EndpointShelf, ShelfID: shelfID}, nil
	}
	return model.Endpoint{}, &CommitGoneError{SHA: sha}
}

// shelfCompareFiles lists the files that differ when at least one side is a
// frozen shelf entry (left = older, right = newer, tree-diff conventions:
// only-in-left → D, only-in-right → A, differing bytes → M, identical →
// omitted). shelf↔commit is scoped to the shelf's member paths — the frozen
// tar cannot speak for paths the shelved commit never changed. Deliberately
// NOT wrapped in one query(): each underlying read (ShelfCommitFiles,
// TreeFiles, ShowFile, ResolveBytes) takes its own Read reservation, and
// nesting a gated read inside a held reservation can deadlock behind a
// queued writer.
func (s *Service) shelfCompareFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error) {
	if left.Kind == model.EndpointShelf && right.Kind == model.EndpointShelf {
		return s.shelfShelfCompare(ctx, left.ShelfID, right.ShelfID)
	}
	if left.Kind == model.EndpointShelf {
		return s.shelfCommitCompare(ctx, left.ShelfID, right.Hash, false)
	}
	return s.shelfCommitCompare(ctx, right.ShelfID, left.Hash, true)
}

func (s *Service) shelfShelfCompare(ctx context.Context, leftID, rightID string) ([]model.CommitFile, error) {
	lf, err := s.ShelfCommitFiles(ctx, leftID)
	if err != nil {
		return nil, err
	}
	rf, err := s.ShelfCommitFiles(ctx, rightID)
	if err != nil {
		return nil, err
	}
	inRight := make(map[string]bool, len(rf))
	for _, f := range rf {
		inRight[f.Path] = true
	}
	inLeft := make(map[string]bool, len(lf))
	var out []model.CommitFile
	for _, f := range lf {
		inLeft[f.Path] = true
		if !inRight[f.Path] {
			out = append(out, model.CommitFile{Status: "D", Path: f.Path})
			continue
		}
		lb, err := s.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: leftID, Path: f.Path})
		if err != nil {
			return nil, err
		}
		rb, err := s.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: rightID, Path: f.Path})
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(lb, rb) {
			out = append(out, model.CommitFile{Status: "M", Path: f.Path})
		}
	}
	for _, f := range rf {
		if !inLeft[f.Path] {
			out = append(out, model.CommitFile{Status: "A", Path: f.Path})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// shelfCommitCompare compares a frozen shelf entry against a live commit,
// scoped to the shelf's members. shelfIsRight names the direction: false =
// shelf is the left/older side (a member missing from the commit tree reads
// as deleted), true = shelf is the right/newer side (missing reads as added).
func (s *Service) shelfCommitCompare(ctx context.Context, shelfID, commitHash string, shelfIsRight bool) ([]model.CommitFile, error) {
	members, err := s.ShelfCommitFiles(ctx, shelfID)
	if err != nil {
		return nil, err
	}
	tree, err := s.TreeFiles(ctx, commitHash)
	if err != nil {
		return nil, err
	}
	inTree := make(map[string]bool, len(tree))
	for _, f := range tree {
		inTree[f.Path] = true
	}
	missing := "D"
	if shelfIsRight {
		missing = "A"
	}
	var out []model.CommitFile
	for _, f := range members {
		if !inTree[f.Path] {
			out = append(out, model.CommitFile{Status: missing, Path: f.Path})
			continue
		}
		sb, err := s.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: shelfID, Path: f.Path})
		if err != nil {
			return nil, err
		}
		cb, err := s.ShowFile(ctx, commitHash, f.Path)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(sb, cb) {
			out = append(out, model.CommitFile{Status: "M", Path: f.Path})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// ComparePatch renders a unified diff for an endpoint pair. Live pairs go
// through git directly (one invocation); a pair involving a frozen shelf
// side is materialized per differing file into temp files and diffed with
// git diff --no-index, headers relabelled to a/<path> b/<path> (the MCP
// gg_compare_file precedent — git cannot see the tar).
func (s *Service) ComparePatch(ctx context.Context, left, right model.Endpoint) (string, error) {
	if left.Kind != model.EndpointShelf && right.Kind != model.EndpointShelf {
		return s.DiffPatch(ctx, livePairSpec(left, right))
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
		lb := s.sideBytes(ctx, left, f.Path) // nil = absent on that side
		rb := s.sideBytes(ctx, right, f.Path)
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

// sideBytes resolves one endpoint's bytes for path, treating any miss as
// absent (empty) — the file list already told us which side holds the file,
// and a diff against empty renders the full add/delete correctly.
func (s *Service) sideBytes(ctx context.Context, e model.Endpoint, path string) []byte {
	data, err := s.ResolveBytes(ctx, e.FileRef(path))
	if err != nil {
		return nil
	}
	return data
}

// livePairSpec maps a non-shelf endpoint pair onto the DiffSpec vocabulary.
// Callers guarantee the pair is one of the forward forms validComparePair
// accepts (commit↔commit, commit→index, commit→worktree, index→worktree).
func livePairSpec(left, right model.Endpoint) model.DiffSpec {
	switch {
	case left.Kind == model.EndpointCommit && right.Kind == model.EndpointCommit:
		return model.DiffSpec{Rev: left.Hash + ".." + right.Hash}
	case left.Kind == model.EndpointCommit && right.Kind == model.EndpointIndex:
		return model.DiffSpec{Cached: true, Rev: left.Hash}
	case left.Kind == model.EndpointCommit: // → worktree
		return model.DiffSpec{Rev: left.Hash}
	default: // index → worktree
		return model.DiffSpec{}
	}
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
