package mcp

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/model"
)

// fileStateProto maps a FileState to its stable protocol name.
func fileStateProto(st model.FileState) string {
	switch st {
	case model.StateCommitted:
		return "committed"
	case model.StateShelf:
		return "shelf"
	case model.StateStaged:
		return "staged"
	case model.StateUntracked:
		return "untracked"
	default:
		return "unstaged"
	}
}

type bookmarkRow struct {
	ID       string `json:"id"`
	Display  string `json:"display"`
	State    string `json:"state"`
	Worktree string `json:"worktree,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Commit   string `json:"commit,omitempty"`
	ShelfID  string `json:"shelf_id,omitempty"`
	Path     string `json:"path,omitempty"`
	Label    string `json:"label,omitempty"`
	IsCommit bool   `json:"is_commit"`
	Created  string `json:"created,omitempty"`
}

func bookmarkRowFrom(b model.Bookmark) bookmarkRow {
	r := bookmarkRow{
		ID: b.ID, Display: b.Address().Display(), State: fileStateProto(b.State),
		Worktree: b.Worktree, Branch: b.Branch, Commit: b.Commit,
		ShelfID: b.ShelfID, Path: b.Path, Label: b.Label, IsCommit: b.IsCommit(),
	}
	if !b.Created.IsZero() {
		r.Created = b.Created.UTC().Format(time.RFC3339)
	}
	return r
}

type bookmarksListIn struct {
	Skip  int `json:"skip,omitempty"`
	Limit int `json:"limit,omitempty"`
}

type bookmarksListOut struct {
	Repo      RepoInfo      `json:"repo"`
	Bookmarks []bookmarkRow `json:"bookmarks"`
}

type bookmarkIDIn struct {
	ID string `json:"id"`
}

type bookmarkGetOut struct {
	Repo     RepoInfo    `json:"repo"`
	Bookmark bookmarkRow `json:"bookmark"`
}

type bookmarkReadIn struct {
	ID       string `json:"id"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

type bookmarkReadOut struct {
	Repo RepoInfo `json:"repo"`
	filePayload
}

func (s *Server) registerBookmarkTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_bookmarks_list",
		Description: "List gg bookmarks (rich file/commit references saved by the user). Paged: skip/limit, limit defaults to 100.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in bookmarksListIn) (*sdk.CallToolResult, bookmarksListOut, error) {
		out := bookmarksListOut{Repo: s.repoInfo(), Bookmarks: []bookmarkRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 100
		}
		if in.Skip < 0 {
			in.Skip = 0
		}
		bs, err := s.svc.BookmarkList(ctx, in.Skip, limit)
		if err != nil {
			return nil, out, fmt.Errorf("listing bookmarks: %v", err)
		}
		for _, b := range bs {
			out.Bookmarks = append(out.Bookmarks, bookmarkRowFrom(b))
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_bookmark_get",
		Description: "Full metadata of one gg bookmark by id.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in bookmarkIDIn) (*sdk.CallToolResult, bookmarkGetOut, error) {
		out := bookmarkGetOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" {
			return nil, out, fmt.Errorf("id is required")
		}
		b, err := s.svc.BookmarkGet(ctx, in.ID)
		if err != nil {
			return nil, out, fmt.Errorf("bookmark not found: %s", in.ID)
		}
		out.Bookmark = bookmarkRowFrom(b)
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_bookmark_read",
		Description: "Read a bookmarked file's content (text; binary is flagged and read via gg_export instead). max_bytes caps the text, default 262144.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in bookmarkReadIn) (*sdk.CallToolResult, bookmarkReadOut, error) {
		out := bookmarkReadOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" {
			return nil, out, fmt.Errorf("id is required")
		}
		b, err := s.svc.BookmarkGet(ctx, in.ID)
		if err != nil {
			return nil, out, fmt.Errorf("bookmark not found: %s", in.ID)
		}
		if b.IsCommit() {
			return nil, out, fmt.Errorf("bookmark %s is a commit pointer — nothing to read; use gg_export to copy the commit's files", in.ID)
		}
		data, err := s.svc.BookmarkBytes(ctx, b)
		if err != nil {
			return nil, out, fmt.Errorf("reading bookmark %s: %v", in.ID, err)
		}
		out.filePayload = textPayload(data, in.MaxBytes)
		return nil, out, nil
	})
}
