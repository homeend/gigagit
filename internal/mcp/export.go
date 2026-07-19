package mcp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

type exportIn struct {
	Bookmark  string `json:"bookmark,omitempty"` // bookmark id
	Shelf     string `json:"shelf,omitempty"`    // shelf entry id
	Dir       string `json:"dir,omitempty"`      // absolute target; default = gg's temp-export area
	Overwrite bool   `json:"overwrite,omitempty"`
}

type exportOut struct {
	Repo  RepoInfo `json:"repo"`
	Dir   string   `json:"dir"`
	Files []string `json:"files"`
	Count int      `json:"count"`
}

func (s *Server) registerExportTool(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_export",
		Description: "Copy a bookmark or shelf entry (a file, or a whole shelved/bookmarked commit's files) into a local directory. Exactly one of bookmark/shelf. dir defaults to gg's temp-export area; an existing dir is refused unless overwrite:true. Never touches the repository.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in exportIn) (*sdk.CallToolResult, exportOut, error) {
		out := exportOut{Repo: s.repoInfo(), Files: []string{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if (in.Bookmark == "") == (in.Shelf == "") {
			return nil, out, fmt.Errorf("pass exactly one of bookmark (id) or shelf (entry id)")
		}
		var (
			files  []model.ExportFile
			subdir string
		)
		if in.Bookmark != "" {
			b, err := s.svc.BookmarkGet(ctx, in.Bookmark)
			if err != nil {
				return nil, out, fmt.Errorf("bookmark not found: %s", in.Bookmark)
			}
			files, subdir, err = s.svc.ExportBookmark(ctx, b)
			if err != nil {
				return nil, out, fmt.Errorf("collecting bookmark %s: %v", in.Bookmark, err)
			}
		} else {
			entry, err := s.svc.ShelfFind(ctx, in.Shelf)
			if err != nil {
				return nil, out, fmt.Errorf("shelf entry not found: %s", in.Shelf)
			}
			files, subdir, err = s.svc.ExportShelfEntry(ctx, entry)
			if err != nil {
				return nil, out, fmt.Errorf("collecting shelf entry %s: %v", in.Shelf, err)
			}
		}
		dir := in.Dir
		if dir == "" {
			base, err := s.svc.TempExportBase(ctx)
			if err != nil {
				return nil, out, fmt.Errorf("resolving the default export dir: %v — pass dir explicitly", err)
			}
			dir = filepath.Join(base, subdir)
		}
		policy := map[string]string{"overwrite": "cancel"}
		if in.Overwrite {
			policy["overwrite"] = "overwrite"
		}
		if _, err := runOp(ctx, s.svc, engine.ExportToDir{Dir: dir, Files: files}, staticDecider{policy: policy}); err != nil {
			if errors.Is(err, engine.ErrExportCancelled) {
				return nil, out, fmt.Errorf("directory exists: %s — pass overwrite:true to replace its contents", dir)
			}
			return nil, out, fmt.Errorf("export failed: %v", err)
		}
		out.Dir = dir
		for _, f := range files {
			out.Files = append(out.Files, f.RelPath)
		}
		out.Count = len(files)
		return nil, out, nil
	})
}
