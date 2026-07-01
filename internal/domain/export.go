package domain

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// TempExportBase is the fixed sibling directory copy-to-temp-dir writes under:
// the MAIN worktree root plus ".tmp" (e.g. /a/x/repo -> /a/x/repo.tmp), anchored
// on the main worktree so it is the repo's sibling even from a linked worktree.
func (s *Service) TempExportBase(ctx context.Context) (string, error) {
	wts, err := s.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	if len(wts) == 0 || wts[0].Path == "" {
		return "", fmt.Errorf("temp export: no main worktree")
	}
	return filepath.Clean(wts[0].Path) + ".tmp", nil
}

// archiveFiles is the gated tar-of-changed-files read used by commit export
// (ShelfAddCommit, ExportBookmark). Mirrors the Read-reservation pattern used
// by CommitFiles/ShowFile in query.go.
func (s *Service) archiveFiles(ctx context.Context, rev string, paths []string) ([]byte, error) {
	return query(ctx, s, "archive:"+rev+":"+strings.Join(paths, "\x00"), func(ctx context.Context) ([]byte, error) {
		return s.repo.ArchiveFiles(ctx, rev, paths)
	})
}

// commitChangedPaths returns the repo-relative paths a commit adds or modifies
// (deletions are dropped: they have no content at the commit to archive). Renames
// and copies contribute their NEW path (CommitFile.Path).
func (s *Service) commitChangedPaths(ctx context.Context, sha string) ([]string, error) {
	files, err := s.CommitFiles(ctx, sha) // gated CommitFiles read (already exists)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		if f.Status == "D" { // deleted at this commit: no content to archive
			continue
		}
		if f.Path != "" {
			out = append(out, f.Path)
		}
	}
	return out, nil
}

// extractTar unpacks a tar archive into ExportFiles (regular files only).
func extractTar(data []byte) ([]model.ExportFile, error) {
	tr := tar.NewReader(bytes.NewReader(data))
	var out []model.ExportFile
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		out = append(out, model.ExportFile{RelPath: filepath.Clean(h.Name), Data: b})
	}
	return out, nil
}

// ExportShelfEntry resolves a shelf entry into the files to write plus the
// default target subdir name. A commit entry extracts its stored tar (durable,
// no git); a file entry is a single ExportFile at its origin path.
func (s *Service) ExportShelfEntry(ctx context.Context, e model.ShelfEntry) ([]model.ExportFile, string, error) {
	if e.IsCommit() {
		blob, err := s.ShelfBlob(ctx, e.ID)
		if err != nil {
			return nil, "", err
		}
		files, err := extractTar(blob)
		if err != nil {
			return nil, "", err
		}
		return files, commitDirName(e.Origin.Commit), nil
	}
	data, err := s.ShelfBlob(ctx, e.ID)
	if err != nil {
		return nil, "", err
	}
	return []model.ExportFile{{RelPath: e.Origin.Path, Data: data}}, sanitizeName(e.ID), nil
}

// ExportBookmark resolves a bookmark into files + default subdir name. A commit
// bookmark archives the commit's changed files live (bookmarks are
// live-by-address); a file bookmark is one ExportFile.
func (s *Service) ExportBookmark(ctx context.Context, b model.Bookmark) ([]model.ExportFile, string, error) {
	if b.IsCommit() {
		paths, err := s.commitChangedPaths(ctx, b.Commit)
		if err != nil {
			return nil, "", err
		}
		if len(paths) == 0 {
			return nil, "", fmt.Errorf("export: commit %s changes no files", b.Commit)
		}
		tar, err := s.archiveFiles(ctx, b.Commit, paths)
		if err != nil {
			return nil, "", err
		}
		files, err := extractTar(tar)
		if err != nil {
			return nil, "", err
		}
		return files, commitDirName(b.Commit), nil
	}
	data, err := s.BookmarkBytes(ctx, b)
	if err != nil {
		return nil, "", err
	}
	name := "bookmark-" + sanitizeName(firstNonEmpty(b.Label, b.ID))
	return []model.ExportFile{{RelPath: b.Path, Data: data}}, name, nil
}

func commitDirName(sha string) string {
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return "commit-" + sha
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// sanitizeName reduces a label/id to a safe single path segment.
func sanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, s)
	s = strings.Trim(s, "-")
	if s == "" {
		return "unshelf"
	}
	return s
}
