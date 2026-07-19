package mcp

import (
	"context"
	"fmt"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/engine"
)

type cherryPickSourceIn struct {
	Shelf    string `json:"shelf,omitempty"`    // shelf entry id (commit entry)
	Bookmark string `json:"bookmark,omitempty"` // bookmark id (commit pointer)
}

type cherryPickIn struct {
	Source     cherryPickSourceIn `json:"source"`
	OnConflict string             `json:"on_conflict,omitempty"` // abort (default) | keep
	Mode       string             `json:"mode,omitempty"`        // auto (default) | patch
}

type cherryPickOut struct {
	Repo            RepoInfo `json:"repo"`
	Lane            string   `json:"lane"` // live|patch
	Commit          string   `json:"commit"`
	Subject         string   `json:"subject,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	Conflicts       bool     `json:"conflicts,omitempty"`
	ConflictedFiles []string `json:"conflicted_files,omitempty"`
}

func (s *Server) registerCherryPickTool(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_cherry_pick",
		Description: "Re-apply a shelved or bookmarked COMMIT onto the current branch. " +
			"Live cherry-pick while the commit object exists; a shelved commit whose object was " +
			"gc'd (or mode:\"patch\") replays its stored patch atomically (git am --3way). " +
			"on_conflict: \"abort\" (default, rolls back) or \"keep\" (leaves conflict markers " +
			"in the tree and reports the conflicted files). MUTATES the repository.",
		Annotations: mutatingAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in cherryPickIn) (*sdk.CallToolResult, cherryPickOut, error) {
		out := cherryPickOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if (in.Source.Shelf == "") == (in.Source.Bookmark == "") {
			return nil, out, fmt.Errorf("pass exactly one of source.shelf (entry id) or source.bookmark (id)")
		}
		var policy map[string]string
		switch in.OnConflict {
		case "", "abort":
			policy = map[string]string{"cherry-pick-conflict": "abort"}
		case "keep":
			policy = map[string]string{"cherry-pick-conflict": "keep-conflicts"}
		default:
			return nil, out, fmt.Errorf(`on_conflict must be "abort" or "keep" (got %q)`, in.OnConflict)
		}
		switch in.Mode {
		case "", "auto", "patch":
		default:
			return nil, out, fmt.Errorf(`mode must be "auto" or "patch" (got %q)`, in.Mode)
		}

		// Resolve the source to a commit sha (+ patch availability for shelf).
		var (
			sha      string
			label    string // shelve-time label; patch-lane subject fallback
			hasPatch bool
			shelfID  string
		)
		if in.Source.Shelf != "" {
			entry, err := s.svc.ShelfFind(ctx, in.Source.Shelf)
			if err != nil {
				return nil, out, fmt.Errorf("shelf entry not found: %s", in.Source.Shelf)
			}
			if !entry.IsCommit() {
				return nil, out, fmt.Errorf("shelf entry %s is a file entry — use gg_write_to_worktree to restore it", in.Source.Shelf)
			}
			sha, label, hasPatch, shelfID = entry.Origin.Commit, entry.Label, entry.PatchSHA != "", entry.ID
		} else {
			if in.Mode == "patch" {
				return nil, out, fmt.Errorf(`mode:"patch" needs a shelf source — bookmarks store no patch`)
			}
			b, err := s.svc.BookmarkGet(ctx, in.Source.Bookmark)
			if err != nil {
				return nil, out, fmt.Errorf("bookmark not found: %s", in.Source.Bookmark)
			}
			if !b.IsCommit() {
				return nil, out, fmt.Errorf("bookmark %s is a file bookmark — use gg_write_to_worktree to paste it", in.Source.Bookmark)
			}
			sha = b.Commit
		}
		out.Commit = shortSha(sha)

		line, found, err := s.svc.CommitLookup(ctx, sha)
		if err != nil {
			return nil, out, fmt.Errorf("resolving %s: %v", shortSha(sha), err)
		}

		if found && in.Mode != "patch" { // live lane
			out.Lane, out.Commit, out.Subject = "live", line.Hash, line.Subject
			res, opErr := runOp(ctx, s.svc, engine.CherryPick{Commit: sha}, staticDecider{policy: policy})
			out.Summary = res.Summary
			switch {
			case opErr == nil && res.Changed: // applied cleanly
				return nil, out, nil
			case opErr == nil && !res.Changed: // conflict hit, policy aborted, tree rolled back
				return nil, out, fmt.Errorf("cherry-pick hit conflicts and was aborted — retry with on_conflict:\"keep\" to keep them in the tree")
			case res.Changed: // conflicts left in the tree (keep, or a preserved-stash restore conflict)
				out.Conflicts = true
				if st, sErr := s.svc.Status(ctx); sErr == nil {
					for _, f := range st.Conflicts() {
						out.ConflictedFiles = append(out.ConflictedFiles, f.Path)
					}
				}
				return nil, out, nil
			default:
				return nil, out, fmt.Errorf("cherry-pick failed: %v", opErr)
			}
		}

		// Patch lane: shelf only.
		if shelfID == "" {
			return nil, out, fmt.Errorf("commit %s no longer exists and this bookmark stores no patch — only a shelf entry with a stored patch can be replayed; shelve commits you may want to restore later", shortSha(sha))
		}
		if !hasPatch {
			return nil, out, fmt.Errorf("commit %s no longer exists and shelf entry %s has no stored patch (shelved before patch support, or a merge commit) — use gg_export to recover its files", shortSha(sha), shelfID)
		}
		tmp, err := s.svc.ShelfPatchFile(ctx, shelfID)
		if err != nil {
			return nil, out, fmt.Errorf("materializing the stored patch: %v", err)
		}
		defer os.Remove(tmp)
		out.Lane, out.Subject = "patch", label
		res, opErr := runOp(ctx, s.svc, engine.ApplyPatch{Path: tmp, Mode: engine.ApplyModeCommits}, staticDecider{policy: map[string]string{}})
		out.Summary = res.Summary
		if opErr != nil { // ApplyModeCommits is atomic: any failure rolled back
			return nil, out, fmt.Errorf("patch replay failed (branch unchanged): %v", opErr)
		}
		return nil, out, nil
	})
}

// shortSha trims a full sha for display (the CLI's convention).
func shortSha(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
