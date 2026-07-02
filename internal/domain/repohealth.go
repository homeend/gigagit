package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// RepoHealth gathers the cheap facts behind the notification center's health
// checks, under a Read reservation: pack size and commit-graph presence are
// filesystem stats on the git common dir; fetch.writeCommitGraph is read at
// both explicit scopes so an inherited global "true" also counts as set.
func (s *Service) RepoHealth(ctx context.Context) (model.RepoHealth, error) {
	return query(ctx, s, "repohealth", func(ctx context.Context) (model.RepoHealth, error) {
		var h model.RepoHealth
		cd, err := s.repo.GitCommonDir(ctx)
		if err != nil {
			return h, err
		}
		h.GitCommonDir = strings.TrimSpace(cd)
		h.PackBytes = packBytes(filepath.Join(h.GitCommonDir, "objects", "pack"))
		h.HasCommitGraph = pathExists(filepath.Join(h.GitCommonDir, "objects", "info", "commit-graph")) ||
			pathExists(filepath.Join(h.GitCommonDir, "objects", "info", "commit-graphs"))
		if v, set, _ := s.repo.ConfigGet(ctx, git.ConfigLocal, "fetch.writeCommitGraph"); set {
			h.WriteCommitGraphSet, h.WriteCommitGraphValue = true, v
		} else if v, set, _ := s.repo.ConfigGet(ctx, git.ConfigGlobal, "fetch.writeCommitGraph"); set {
			h.WriteCommitGraphSet, h.WriteCommitGraphValue = true, v
		}
		return h, nil
	})
}

// packBytes sums the *.pack files in dir (flat — git keeps packs in one
// directory). A missing dir reads as 0, not an error.
func packBytes(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pack") {
			continue
		}
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
