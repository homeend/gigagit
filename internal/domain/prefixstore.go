package domain

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/prefix"
	"github.com/homeend/gigagit/internal/template"
)

// PrefixStatePath overrides the prefix root dir. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var PrefixStatePath string

// SetPrefixStores injects both scope stores (tests).
func (s *Service) SetPrefixStores(global, repo prefix.Store) {
	s.mu.Lock()
	s.prefixGlobal = global
	s.prefixRepo = repo
	s.mu.Unlock()
}

func (s *Service) prefixStores(ctx context.Context) (global, repo prefix.Store) {
	s.mu.Lock()
	if s.prefixGlobal != nil || s.prefixRepo != nil {
		g, r := s.prefixGlobal, s.prefixRepo
		s.mu.Unlock()
		return g, r
	}
	s.mu.Unlock()

	base := PrefixStatePath
	if base == "" {
		base = prefixBaseDir()
	}
	if base == "" {
		return nil, nil // prefixes disabled (no state dir)
	}
	g := prefix.NewFileStore(filepath.Join(base, "global"), model.ProfileScopeGlobal)
	key := "unknown"
	if cd, err := s.GitCommonDir(ctx); err == nil {
		key = repoKey(strings.TrimSpace(cd))
	}
	r := prefix.NewFileStore(filepath.Join(base, key), model.ProfileScopeRepo)

	s.mu.Lock()
	s.prefixGlobal, s.prefixRepo = g, r
	s.mu.Unlock()
	return g, r
}

// prefixBaseDir resolves <state>/gg/prefix cross-platform (mirrors profileBaseDir).
func prefixBaseDir() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "prefix")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "prefix")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "prefix")
}

// Prefixes lists global rows then repo rows, each tagged with its scope.
func (s *Service) Prefixes(ctx context.Context) ([]model.Prefix, error) {
	global, repo := s.prefixStores(ctx)
	var out []model.Prefix
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

// AddPrefix validates the value's tokens, then routes to the scope's store.
func (s *Service) AddPrefix(ctx context.Context, p model.Prefix) (model.Prefix, error) {
	if err := ValidatePrefixValue(p.Value); err != nil {
		return model.Prefix{}, err
	}
	global, repo := s.prefixStores(ctx)
	st := global
	if p.Scope == model.ProfileScopeRepo {
		st = repo
	}
	if st == nil {
		return model.Prefix{}, os.ErrInvalid
	}
	return st.Add(p)
}

// PrefixID returns the stable id derived from a prefix value. Frontends (the
// CLI) identify a prefix by its value but must not import internal/prefix
// directly (archtest-guarded), so they route the slugging through here.
func PrefixID(value string) string { return prefix.PrefixID(value) }

// RemovePrefix removes id from the store matching scope.
func (s *Service) RemovePrefix(ctx context.Context, scope model.ProfileScope, id string) error {
	global, repo := s.prefixStores(ctx)
	st := global
	if scope == model.ProfileScopeRepo {
		st = repo
	}
	if st == nil {
		return os.ErrInvalid
	}
	return st.Remove(id)
}

// ValidatePrefixValue rejects an empty value, the <branch> token, and any
// unknown/malformed token. Well-formedness is proven by a dry resolve with
// placeholder inputs for every <user:…> label and 0 for every <seq:…> counter.
func ValidatePrefixValue(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("invalid prefix: empty value")
	}
	if strings.Contains(value, "<branch>") {
		return fmt.Errorf("invalid prefix: <branch> is not allowed in a prefix")
	}
	inputs := map[string]string{}
	for _, l := range template.UserLabels(value) {
		inputs[l] = "x"
	}
	seqs := map[string]int{}
	for _, n := range template.SeqNames(value) {
		seqs[n] = 0
	}
	ctx := template.Ctx{
		ParentBranch: "parent",
		Repo:         "repo",
		Seqs:         seqs,
		Now:          time.Now,
		Rand:         rand.New(rand.NewPCG(1, 2)),
	}
	if _, err := template.Resolve(value, inputs, ctx); err != nil {
		return fmt.Errorf("invalid prefix: %w", err)
	}
	return nil
}
