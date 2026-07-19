package mcp

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/model"
)

type shelfBucketRow struct {
	Name   string `json:"name"`
	Hidden bool   `json:"hidden,omitempty"`
}

type shelfBucketsOut struct {
	Repo    RepoInfo         `json:"repo"`
	Buckets []shelfBucketRow `json:"buckets"`
}

type shelfListIn struct {
	Bucket string `json:"bucket,omitempty"`
	Skip   int    `json:"skip,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type shelfRow struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"` // file|commit
	OriginDisplay string `json:"origin_display"`
	Label         string `json:"label,omitempty"`
	Path          string `json:"path,omitempty"`
	Commit        string `json:"commit,omitempty"`
	Size          int64  `json:"size"`
	HasPatch      bool   `json:"has_patch"`
	CreatedAt     string `json:"created_at,omitempty"`
}

func shelfRowFrom(e model.ShelfEntry) shelfRow {
	kind := "file"
	if e.IsCommit() {
		kind = "commit"
	}
	r := shelfRow{
		ID: e.ID, Kind: kind, OriginDisplay: e.Origin.Display(), Label: e.Label,
		Path: e.Origin.Path, Commit: e.Origin.Commit, Size: e.Size,
		HasPatch: e.PatchSHA != "",
	}
	if !e.Created.IsZero() {
		r.CreatedAt = e.Created.UTC().Format(time.RFC3339)
	}
	return r
}

type shelfListOut struct {
	Repo    RepoInfo   `json:"repo"`
	Entries []shelfRow `json:"entries"`
}

type shelfIDIn struct {
	ID string `json:"id"`
}

type shelfCommitFilesOut struct {
	Repo  RepoInfo        `json:"repo"`
	Files []commitFileRow `json:"files"`
}

type shelfReadIn struct {
	ID       string `json:"id"`
	Member   string `json:"member,omitempty"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

type shelfReadOut struct {
	Repo RepoInfo `json:"repo"`
	filePayload
}

func (s *Server) registerShelfTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_shelf_buckets",
		Description: "List gg shelf buckets (named groups of shelved files/commits).",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, shelfBucketsOut, error) {
		out := shelfBucketsOut{Repo: s.repoInfo(), Buckets: []shelfBucketRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		bs, err := s.svc.ShelfBuckets(ctx)
		if err != nil {
			return nil, out, fmt.Errorf("listing shelf buckets: %v", err)
		}
		for _, b := range bs {
			out.Buckets = append(out.Buckets, shelfBucketRow{Name: b.Name, Hidden: b.Hidden})
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_shelf_list",
		Description: "List shelf entries in a bucket (default \"default\"). kind is \"file\" (one shelved file) or \"commit\" (a frozen commit snapshot). Paged: skip/limit, limit defaults to 100.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in shelfListIn) (*sdk.CallToolResult, shelfListOut, error) {
		out := shelfListOut{Repo: s.repoInfo(), Entries: []shelfRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		bucket := in.Bucket
		if bucket == "" {
			bucket = "default"
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 100
		}
		if in.Skip < 0 {
			in.Skip = 0
		}
		es, err := s.svc.ShelfList(ctx, bucket, in.Skip, limit)
		if err != nil {
			return nil, out, fmt.Errorf("listing shelf bucket %q: %v", bucket, err)
		}
		for _, e := range es {
			out.Entries = append(out.Entries, shelfRowFrom(e))
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_shelf_commit_files",
		Description: "List the member files of a shelved COMMIT entry (path, status letter, old_path for renames).",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in shelfIDIn) (*sdk.CallToolResult, shelfCommitFilesOut, error) {
		out := shelfCommitFilesOut{Repo: s.repoInfo(), Files: []commitFileRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" {
			return nil, out, fmt.Errorf("id is required")
		}
		entry, err := s.svc.ShelfFind(ctx, in.ID)
		if err != nil {
			return nil, out, fmt.Errorf("shelf entry not found: %s", in.ID)
		}
		if !entry.IsCommit() {
			return nil, out, fmt.Errorf("shelf entry %s is a file entry — use gg_shelf_read without member", in.ID)
		}
		files, err := s.svc.ShelfCommitFiles(ctx, in.ID)
		if err != nil {
			return nil, out, fmt.Errorf("listing members of %s: %v", in.ID, err)
		}
		for _, f := range files {
			out.Files = append(out.Files, commitFileRowFrom(f))
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_shelf_read",
		Description: "Read a shelf entry's content: a file entry's bytes, or ONE member of a commit entry (member = repo-relative path from gg_shelf_commit_files). Text only; binary is flagged. max_bytes caps the text, default 262144.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in shelfReadIn) (*sdk.CallToolResult, shelfReadOut, error) {
		out := shelfReadOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" {
			return nil, out, fmt.Errorf("id is required")
		}
		entry, err := s.svc.ShelfFind(ctx, in.ID)
		if err != nil {
			return nil, out, fmt.Errorf("shelf entry not found: %s", in.ID)
		}
		var data []byte
		switch {
		case entry.IsCommit() && in.Member == "":
			return nil, out, fmt.Errorf("shelf entry %s is a commit — pass member (list members with gg_shelf_commit_files)", in.ID)
		case !entry.IsCommit() && in.Member != "":
			return nil, out, fmt.Errorf("shelf entry %s is a file entry — omit member", in.ID)
		case entry.IsCommit():
			data, err = s.svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: in.ID, Path: in.Member})
			if err != nil {
				return nil, out, fmt.Errorf("reading member %q of %s: %v", in.Member, in.ID, err)
			}
		default:
			data, err = s.svc.ShelfBlob(ctx, in.ID)
			if err != nil {
				return nil, out, fmt.Errorf("reading shelf entry %s: %v", in.ID, err)
			}
		}
		out.filePayload = textPayload(data, in.MaxBytes)
		return nil, out, nil
	})
}

// commitFileRow is the shared changed-file reply shape (shelf commit members,
// compare results): status letter A/M/D/R/C/T plus the pre-rename path.
type commitFileRow struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	OldPath string `json:"old_path,omitempty"`
}

func commitFileRowFrom(f model.CommitFile) commitFileRow {
	return commitFileRow{Path: f.Path, Status: f.Status, OldPath: f.OldPath}
}
