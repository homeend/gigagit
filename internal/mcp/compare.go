package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

type treeSideIn struct {
	Kind string `json:"kind"`          // worktree|index|commit
	Rev  string `json:"rev,omitempty"` // required for kind=commit; any rev-parseable name
}

type compareTreesIn struct {
	Left  treeSideIn `json:"left"`
	Right treeSideIn `json:"right"`
}

type compareTreesOut struct {
	Repo         RepoInfo        `json:"repo"`
	LeftDisplay  string          `json:"left_display"`
	RightDisplay string          `json:"right_display"`
	Files        []commitFileRow `json:"files"`
}

// endpointFor resolves one compare side. A commit rev resolves to its sha
// BEFORE the compare (the resolve-to-tip-hash rule: never key a diff on a
// mutable name).
//
// The sha comes from ResolveRev, not CommitLookup: CommitLookup's hash is `%h`
// (model.LogLine.Hash), whose width honours core.abbrev — git's legal minimum
// is 4, below model.CommitEndpoint's 7..64 floor — so keying the endpoint on it
// made a legal repo config a hard failure. CommitLookup stays what it is
// documented as, the display-facing read. This is the CLI's pattern
// (internal/cli/compare.go).
func (s *Server) endpointFor(ctx context.Context, side treeSideIn) (model.Endpoint, string, error) {
	switch side.Kind {
	case "worktree":
		return model.WorkTreeEndpoint(), "worktree", nil
	case "index":
		return model.IndexEndpoint(), "index", nil
	case "commit":
		if side.Rev == "" {
			return model.Endpoint{}, "", fmt.Errorf("rev is required for kind \"commit\"")
		}
		sha, ok, err := s.svc.ResolveRev(ctx, side.Rev)
		if err != nil {
			return model.Endpoint{}, "", fmt.Errorf("resolving %q: %v", side.Rev, err)
		}
		if !ok {
			return model.Endpoint{}, "", fmt.Errorf("unknown revision: %s", side.Rev)
		}
		ep, err := model.CommitEndpoint(sha)
		if err != nil {
			return model.Endpoint{}, "", fmt.Errorf("resolving %q: %v", side.Rev, err)
		}
		// The label is cosmetic: the rev already resolved, so a lookup miss
		// here only costs the subject, never the compare.
		display := sha
		if line, ok, _ := s.svc.CommitLookup(ctx, sha); ok {
			display = line.Hash + " " + line.Subject
		}
		return ep, display, nil
	default:
		return model.Endpoint{}, "", fmt.Errorf(`kind must be "worktree", "index", or "commit" (got %q)`, side.Kind)
	}
}

type fileSideIn struct {
	Source  string `json:"source"`            // unstaged|staged|commit|shelf|bookmark|link
	Locator string `json:"locator,omitempty"` // rev (commit) or shelf entry id
	ID      string `json:"id,omitempty"`      // bookmark id (source=bookmark)
	Path    string `json:"path,omitempty"`    // repo-relative; member path for shelf commits
	Link    string `json:"link,omitempty"`    // gg:// link (source=link); carries its own path and state
}

type compareFileIn struct {
	Left  fileSideIn `json:"left"`
	Right fileSideIn `json:"right"`
}

type compareFileOut struct {
	Repo         RepoInfo `json:"repo"`
	LeftDisplay  string   `json:"left_display"`
	RightDisplay string   `json:"right_display"`
	Identical    bool     `json:"identical"`
	Binary       bool     `json:"binary,omitempty"`
	LeftSize     int      `json:"left_size,omitempty"`
	RightSize    int      `json:"right_size,omitempty"`
	UnifiedDiff  string   `json:"unified_diff,omitempty"`
}

// resolveFileSide fetches one side's bytes + display label.
func (s *Server) resolveFileSide(ctx context.Context, side fileSideIn) ([]byte, string, error) {
	if side.Source == "bookmark" {
		if side.ID == "" {
			return nil, "", fmt.Errorf("id is required for source \"bookmark\"")
		}
		b, err := s.svc.BookmarkGet(ctx, side.ID)
		if err != nil {
			return nil, "", fmt.Errorf("bookmark not found: %s", side.ID)
		}
		if b.IsCommit() {
			return nil, "", fmt.Errorf("bookmark %s is a commit pointer — compare a file, or use gg_compare_trees against its commit", side.ID)
		}
		data, err := s.svc.BookmarkBytes(ctx, b)
		if err != nil {
			return nil, "", fmt.Errorf("reading bookmark %s: %v", side.ID, err)
		}
		return data, b.Address().Display(), nil
	}
	// A gg:// link carries its own path AND state, so it needs neither the
	// path nor the locator every other source requires — that is the whole
	// point of a link. It is resolved through the same door the link tools
	// use, so a link naming another checkout is refused here too.
	if side.Source == "link" {
		res, err := s.resolveLinkArg(ctx, side.Link)
		if err != nil {
			return nil, "", err
		}
		if res.Addr.Path == "" {
			return nil, "", fmt.Errorf("%s names no file — compare a file link, or use gg_compare_links for a whole place", side.Link)
		}
		data, err := s.svc.ResolveBytes(ctx, res.Addr.FileRef())
		if err != nil {
			return nil, "", fmt.Errorf("reading %s: %v", side.Link, err)
		}
		return data, res.Addr.Display(), nil
	}
	if side.Path == "" {
		return nil, "", fmt.Errorf("path is required for source %q", side.Source)
	}
	ref := model.FileRef{Locator: side.Locator, Path: side.Path}
	display := side.Source + ":" + side.Path
	switch side.Source {
	case "unstaged":
		ref.Source = model.SourceUnstaged
	case "staged":
		ref.Source = model.SourceStaged
	case "commit":
		if side.Locator == "" {
			return nil, "", fmt.Errorf("locator (a revision) is required for source \"commit\"")
		}
		ref.Source = model.SourceCommit
		display = side.Locator + ":" + side.Path
	case "shelf":
		if side.Locator == "" {
			return nil, "", fmt.Errorf("locator (a shelf entry id) is required for source \"shelf\"")
		}
		ref.Source = model.SourceShelf
		display = "shelf:" + side.Locator + ":" + side.Path
	default:
		return nil, "", fmt.Errorf(`source must be "unstaged", "staged", "commit", "shelf", "bookmark", or "link" (got %q)`, side.Source)
	}
	data, err := s.svc.ResolveBytes(ctx, ref)
	if err != nil {
		return nil, "", fmt.Errorf("reading %s: %v", display, err)
	}
	return data, display, nil
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)
}

func (s *Server) registerCompareTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_compare_trees",
		Description: "Whole-tree compare between two endpoints (worktree, index, or a commit by rev): the changed-file list with status letters. Use gg_compare_file for one file's diff.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in compareTreesIn) (*sdk.CallToolResult, compareTreesOut, error) {
		out := compareTreesOut{Repo: s.repoInfo(), Files: []commitFileRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		left, ld, err := s.endpointFor(ctx, in.Left)
		if err != nil {
			return nil, out, fmt.Errorf("left: %v", err)
		}
		right, rd, err := s.endpointFor(ctx, in.Right)
		if err != nil {
			return nil, out, fmt.Errorf("right: %v", err)
		}
		out.LeftDisplay, out.RightDisplay = ld, rd
		// EvalEndpoint + CompareSets, NOT CompareFiles. The two sides are
		// whatever the client named, in whatever order it named them — nothing
		// here constrains the pair — and CompareFiles is a thin wrapper over
		// git's own diff, which only walks forward (a commit, then the index,
		// then the working tree). Asking {left: worktree, right: commit} through
		// it answered "DiffTreeFiles: unsupported endpoint pair 1 → 3" while
		// `gg compare @worktree main` compared, which is one compare frontend
		// disagreeing with another about what a user may ask. CompareSets is
		// total: it asks a reversed pair forward and inverts the answer.
		leftSet, err := s.svc.EvalEndpoint(ctx, left)
		if err != nil {
			return nil, out, fmt.Errorf("left: %v", err)
		}
		rightSet, err := s.svc.EvalEndpoint(ctx, right)
		if err != nil {
			return nil, out, fmt.Errorf("right: %v", err)
		}
		files, err := s.svc.CompareSets(ctx, leftSet, rightSet)
		if err != nil {
			return nil, out, fmt.Errorf("comparing: %v", err)
		}
		for _, f := range files {
			out.Files = append(out.Files, commitFileRowFrom(f))
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_compare_file",
		Description: "Unified diff between two file versions. Each side: {source: unstaged|staged|commit|shelf, locator, path} or {source: bookmark, id}. locator = revision for commit, shelf entry id for shelf (path = member for shelved commits).",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in compareFileIn) (*sdk.CallToolResult, compareFileOut, error) {
		out := compareFileOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		leftData, ld, err := s.resolveFileSide(ctx, in.Left)
		if err != nil {
			return nil, out, fmt.Errorf("left: %v", err)
		}
		rightData, rd, err := s.resolveFileSide(ctx, in.Right)
		if err != nil {
			return nil, out, fmt.Errorf("right: %v", err)
		}
		out.LeftDisplay, out.RightDisplay = ld, rd
		if bytes.Equal(leftData, rightData) {
			out.Identical = true
			return nil, out, nil
		}
		if isBinary(leftData) || isBinary(rightData) {
			out.Binary = true
			out.LeftSize, out.RightSize = len(leftData), len(rightData)
			return nil, out, nil
		}
		dir, err := os.MkdirTemp("", "gg-mcp-diff-*")
		if err != nil {
			return nil, out, fmt.Errorf("temp dir: %v", err)
		}
		defer os.RemoveAll(dir)
		a, b := filepath.Join(dir, "left"), filepath.Join(dir, "right")
		if err := os.WriteFile(a, leftData, 0o600); err != nil {
			return nil, out, fmt.Errorf("temp file: %v", err)
		}
		if err := os.WriteFile(b, rightData, 0o600); err != nil {
			return nil, out, fmt.Errorf("temp file: %v", err)
		}
		diff, err := s.svc.DiffNoIndex(ctx, a, b)
		if err != nil {
			return nil, out, fmt.Errorf("diffing: %v", err)
		}
		out.UnifiedDiff = domain.RelabelNoIndexDiff(diff, ld, rd)
		return nil, out, nil
	})
}
