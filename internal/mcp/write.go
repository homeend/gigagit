package mcp

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

type writeSourceIn struct {
	Shelf    string `json:"shelf,omitempty"`    // shelf entry id
	Member   string `json:"member,omitempty"`   // member path inside a shelved commit
	Bookmark string `json:"bookmark,omitempty"` // bookmark id (file bookmark)
}

type writeIn struct {
	Source    writeSourceIn `json:"source"`
	Path      string        `json:"path,omitempty"` // repo-relative destination; default = origin path
	Overwrite bool          `json:"overwrite,omitempty"`
}

type writeOut struct {
	Repo      RepoInfo `json:"repo"`
	Path      string   `json:"path"`
	Bytes     int      `json:"bytes"`
	Unchanged bool     `json:"unchanged,omitempty"`
}

func (s *Server) registerWriteTool(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_write_to_worktree",
		Description: "Write a stored file version — a shelf file entry, one member of a shelved " +
			"commit, or a file bookmark — into the working tree as an UNSTAGED change. path " +
			"defaults to the entry's own origin path; an existing different file is refused " +
			"unless overwrite:true; identical content is a no-op. MUTATES the working tree.",
		Annotations: mutatingAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in writeIn) (*sdk.CallToolResult, writeOut, error) {
		out := writeOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if (in.Source.Shelf == "") == (in.Source.Bookmark == "") {
			return nil, out, fmt.Errorf("pass exactly one of source.shelf (entry id) or source.bookmark (id)")
		}

		var (
			data       []byte
			originPath string
		)
		if in.Source.Shelf != "" {
			entry, err := s.svc.ShelfFind(ctx, in.Source.Shelf)
			if err != nil {
				return nil, out, fmt.Errorf("shelf entry not found: %s", in.Source.Shelf)
			}
			switch {
			case entry.IsCommit() && in.Source.Member == "":
				return nil, out, fmt.Errorf("shelf entry %s is a commit — pass source.member (list members with gg_shelf_commit_files)", in.Source.Shelf)
			case !entry.IsCommit() && in.Source.Member != "":
				return nil, out, fmt.Errorf("shelf entry %s is a file entry — omit member", in.Source.Shelf)
			case entry.IsCommit():
				data, err = s.svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: in.Source.Shelf, Path: in.Source.Member})
				if err != nil {
					return nil, out, fmt.Errorf("reading member %q of %s: %v", in.Source.Member, in.Source.Shelf, err)
				}
				originPath = in.Source.Member
			default:
				data, err = s.svc.ShelfBlob(ctx, in.Source.Shelf)
				if err != nil {
					return nil, out, fmt.Errorf("reading shelf entry %s: %v", in.Source.Shelf, err)
				}
				originPath = entry.Origin.Path
			}
		} else {
			b, err := s.svc.BookmarkGet(ctx, in.Source.Bookmark)
			if err != nil {
				return nil, out, fmt.Errorf("bookmark not found: %s", in.Source.Bookmark)
			}
			if b.IsCommit() {
				return nil, out, fmt.Errorf("bookmark %s is a commit pointer — use gg_cherry_pick to re-apply it or gg_export to copy its files", in.Source.Bookmark)
			}
			data, err = s.svc.BookmarkBytes(ctx, b)
			if err != nil {
				return nil, out, fmt.Errorf("reading bookmark %s: %v", in.Source.Bookmark, err)
			}
			originPath = b.Path
		}

		dest := in.Path
		if dest == "" {
			dest = originPath
		}
		if dest == "" {
			return nil, out, fmt.Errorf("this source has no origin path — pass path explicitly")
		}
		out.Path, out.Bytes = dest, len(data)

		policy := map[string]string{"overwrite": "cancel"}
		if in.Overwrite {
			policy["overwrite"] = "overwrite"
		}
		res, err := runOp(ctx, s.svc, engine.WriteFile{Path: dest, Data: data}, staticDecider{policy: policy})
		if err != nil {
			if errors.Is(err, engine.ErrWriteCancelled) {
				return nil, out, fmt.Errorf("file exists: %s — pass overwrite:true to replace it", dest)
			}
			return nil, out, fmt.Errorf("write failed: %v", err)
		}
		out.Unchanged = !res.Changed // WriteFile: identical bytes = Changed:false no-op
		return nil, out, nil
	})
}
