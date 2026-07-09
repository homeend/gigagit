package domain

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/model"
)

// ResolveBytes returns the bytes a FileRef points at, dispatching by source.
// Comparing any two refs = resolve both, feed the existing Differ. A shelf ref
// is member-aware: a shelved COMMIT entry with a Path resolves to that one
// file's bytes from the stored tar (see shelfResolve); file entries and
// path-less commit refs stay the whole blob.
func (s *Service) ResolveBytes(ctx context.Context, ref model.FileRef) ([]byte, error) {
	switch ref.Source {
	case model.SourceUnstaged:
		return s.WorktreeFile(ctx, ref.Path)
	case model.SourceStaged:
		return s.ShowFile(ctx, "", ref.Path) // `git show :path` = index blob
	case model.SourceCommit:
		return s.ShowFile(ctx, ref.Locator, ref.Path)
	case model.SourceShelf:
		return s.shelfResolve(ctx, ref.Locator, ref.Path)
	default:
		return nil, fmt.Errorf("resolve: unknown source %d", ref.Source)
	}
}
