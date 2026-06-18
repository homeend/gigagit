package domain

import (
	"context"
	"fmt"

	"github.com/gigagit/gg/internal/model"
)

// ResolveBytes returns the bytes a FileRef points at, dispatching by source.
// Comparing any two refs = resolve both, feed the existing Differ. The
// SourceShelf branch is wired in the shelf-commands task once the Service owns
// a store.
func (s *Service) ResolveBytes(ctx context.Context, ref model.FileRef) ([]byte, error) {
	switch ref.Source {
	case model.SourceUnstaged:
		return s.WorktreeFile(ctx, ref.Path)
	case model.SourceStaged:
		return s.ShowFile(ctx, "", ref.Path) // `git show :path` = index blob
	case model.SourceCommit:
		return s.ShowFile(ctx, ref.Locator, ref.Path)
	case model.SourceShelf:
		// Wired in the shelf-commands task; ShelfBlob lands with the store.
		return nil, fmt.Errorf("resolve: shelf source not yet wired")
	default:
		return nil, fmt.Errorf("resolve: unknown source %d", ref.Source)
	}
}
