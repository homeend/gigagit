package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/profile"
)

// ProfileStatePath overrides the profile root dir. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var ProfileStatePath string

// SetProfileStores injects both scope stores (tests).
func (s *Service) SetProfileStores(global, repo profile.Store) {
	s.mu.Lock()
	s.profileGlobal = global
	s.profileRepo = repo
	s.mu.Unlock()
}

func (s *Service) profileStores(ctx context.Context) (global, repo profile.Store) {
	s.mu.Lock()
	if s.profileGlobal != nil || s.profileRepo != nil {
		g, r := s.profileGlobal, s.profileRepo
		s.mu.Unlock()
		return g, r
	}
	s.mu.Unlock()

	base := ProfileStatePath
	if base == "" {
		base = profileBaseDir()
	}
	if base == "" {
		return nil, nil // profiles disabled (no state dir)
	}
	g := profile.NewFileStore(filepath.Join(base, "global"), model.ProfileScopeGlobal)
	key := "unknown"
	if cd, err := s.GitCommonDir(ctx); err == nil {
		key = repoKey(strings.TrimSpace(cd))
	}
	r := profile.NewFileStore(filepath.Join(base, key), model.ProfileScopeRepo)

	s.mu.Lock()
	s.profileGlobal, s.profileRepo = g, r
	s.mu.Unlock()
	return g, r
}

// profileBaseDir resolves <state>/gg/profile cross-platform (mirrors
// bookmarkBaseDir). "" when no home/state dir exists.
func profileBaseDir() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "profile")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "profile")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "profile")
}

// Profiles lists global rows then repo rows, each tagged with its scope.
func (s *Service) Profiles(ctx context.Context) ([]model.Profile, error) {
	global, repo := s.profileStores(ctx)
	var out []model.Profile
	if global != nil {
		gs, err := global.List()
		if err != nil {
			return nil, err
		}
		out = append(out, gs...)
	}
	if repo != nil {
		rs, err := repo.List()
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	return out, nil
}

// AddProfile routes to the store matching p.Scope.
func (s *Service) AddProfile(ctx context.Context, p model.Profile) (model.Profile, error) {
	global, repo := s.profileStores(ctx)
	st := global
	if p.Scope == model.ProfileScopeRepo {
		st = repo
	}
	if st == nil {
		return model.Profile{}, os.ErrInvalid
	}
	return st.Add(p)
}

// RemoveProfile removes id from the store matching scope.
func (s *Service) RemoveProfile(ctx context.Context, scope model.ProfileScope, id string) error {
	global, repo := s.profileStores(ctx)
	st := global
	if scope == model.ProfileScopeRepo {
		st = repo
	}
	if st == nil {
		return os.ErrInvalid
	}
	return st.Remove(id)
}
