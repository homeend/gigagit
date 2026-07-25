package domain

import (
	"context"
	"os"
	"path/filepath"
	"sort"
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
		if kvs, err := s.repo.ConfigGetRegexp(ctx, `^(branch\..*\.(remote|merge)|remote\..*\.fetch)$`); err == nil && len(kvs) > 0 {
			if branches, err := s.repo.Branches(ctx); err == nil {
				h.UnmappedBranches = unmappedFromConfig(kvs, branches)
			}
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

// unmappedFromConfig joins branch tracking config against live branches:
// a branch is unmapped when branch.<n>.remote is exactly "origin" and
// branch.<n>.merge is set, origin HAS a fetch refspec (a remote with none
// is a different problem), and yet %(upstream:short) resolved to nothing —
// the narrowed-refspec state where pushes never move the remote-tracking
// ref. Detection is scoped to "origin" on purpose: the notice's fix action
// (narrowRefspecNotice) only ever writes remote.origin.fetch and fetches
// from "origin", so a branch tracking a different remote must never be
// listed here — that would forge a bogus origin mapping the fetch can't
// satisfy. Branch names may contain dots, so config keys are parsed from
// both ends (strip "branch." prefix and ".remote"/".merge" suffix), never
// by Split.
func unmappedFromConfig(kvs [][2]string, branches []model.Branch) []string {
	remoteOf := map[string]string{} // branch name → configured remote
	hasMerge := map[string]bool{}   // branch name → branch.<n>.merge present
	originFetchable := false        // remote.origin.fetch present
	for _, kv := range kvs {
		key := kv[0]
		switch {
		case strings.HasPrefix(key, "branch.") && strings.HasSuffix(key, ".remote"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "branch."), ".remote")
			remoteOf[name] = kv[1]
		case strings.HasPrefix(key, "branch.") && strings.HasSuffix(key, ".merge"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "branch."), ".merge")
			hasMerge[name] = true
		case key == "remote.origin.fetch":
			originFetchable = true
		}
	}
	var out []string
	if !originFetchable {
		return out
	}
	for _, b := range branches {
		if b.Upstream != "" {
			continue
		}
		if remoteOf[b.Name] == "origin" && hasMerge[b.Name] {
			out = append(out, b.Name)
		}
	}
	sort.Strings(out)
	return out
}
