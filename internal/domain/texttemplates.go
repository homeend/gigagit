package domain

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/template"
	"github.com/homeend/gigagit/internal/texttmpl"
	"github.com/homeend/gigagit/internal/worktree"
)

// MaxTextTemplateTitle (runes) and MaxTextTemplateBody (bytes) bound a stored
// text template.
const (
	MaxTextTemplateTitle = 80
	MaxTextTemplateBody  = 64 << 10
)

// TextTemplateStatePath overrides the text-template root dir. "" uses the
// default XDG location. cmd/gg leaves it ""; tests point it at a temp dir.
var TextTemplateStatePath string

// SetTextTemplateStores injects both scope stores (tests).
func (s *Service) SetTextTemplateStores(global, repo texttmpl.Store) {
	s.mu.Lock()
	s.textGlobal = global
	s.textRepo = repo
	s.mu.Unlock()
}

func (s *Service) textTemplateStores(ctx context.Context) (global, repo texttmpl.Store) {
	s.mu.Lock()
	if s.textGlobal != nil || s.textRepo != nil {
		g, r := s.textGlobal, s.textRepo
		s.mu.Unlock()
		return g, r
	}
	s.mu.Unlock()

	base := TextTemplateStatePath
	if base == "" {
		base = stateBaseDir("texttemplates")
	}
	if base == "" {
		return nil, nil // text templates disabled (no state dir)
	}
	g := texttmpl.NewFileStore(filepath.Join(base, "global"), model.ProfileScopeGlobal)
	key := "unknown"
	if cd, err := s.GitCommonDir(ctx); err == nil {
		key = repoKey(strings.TrimSpace(cd))
	}
	r := texttmpl.NewFileStore(filepath.Join(base, key), model.ProfileScopeRepo)

	s.mu.Lock()
	s.textGlobal, s.textRepo = g, r
	s.mu.Unlock()
	return g, r
}

// TextTemplates lists global rows then repo rows, each tagged with its scope
// and alphabetical by title within it.
func (s *Service) TextTemplates(ctx context.Context) ([]model.TextTemplate, error) {
	global, repo := s.textTemplateStores(ctx)
	var out []model.TextTemplate
	for _, st := range []texttmpl.Store{global, repo} {
		if st == nil {
			continue
		}
		ts, err := st.List()
		if err != nil {
			return nil, err
		}
		out = append(out, ts...)
	}
	return out, nil
}

func (s *Service) textStore(ctx context.Context, scope model.ProfileScope) (texttmpl.Store, error) {
	global, repo := s.textTemplateStores(ctx)
	st := global
	if scope == model.ProfileScopeRepo {
		st = repo
	}
	if st == nil {
		return nil, os.ErrInvalid
	}
	return st, nil
}

// trimTextTemplate normalises what is stored: the title without surrounding
// space, the body without trailing whitespace.
func trimTextTemplate(t model.TextTemplate) model.TextTemplate {
	t.Title, t.Body = strings.TrimSpace(t.Title), strings.TrimRight(t.Body, " \t\r\n")
	return t
}

// AddTextTemplate validates t, then stores it in t.Scope's store. A title
// already used in that scope is refused (IsTextTemplateDuplicate).
func (s *Service) AddTextTemplate(ctx context.Context, t model.TextTemplate) (model.TextTemplate, error) {
	t = trimTextTemplate(t)
	if err := ValidateTextTemplate(t.Title, t.Body); err != nil {
		return model.TextTemplate{}, err
	}
	st, err := s.textStore(ctx, t.Scope)
	if err != nil {
		return model.TextTemplate{}, err
	}
	return st.Add(t)
}

// UpdateTextTemplate replaces the template stored under id in scope's store.
// The id follows the title, so the returned row may carry a new one.
func (s *Service) UpdateTextTemplate(ctx context.Context, scope model.ProfileScope, id string, t model.TextTemplate) (model.TextTemplate, error) {
	t = trimTextTemplate(t)
	if err := ValidateTextTemplate(t.Title, t.Body); err != nil {
		return model.TextTemplate{}, err
	}
	st, err := s.textStore(ctx, scope)
	if err != nil {
		return model.TextTemplate{}, err
	}
	return st.Update(id, t)
}

// RemoveTextTemplate removes id from the store matching scope.
func (s *Service) RemoveTextTemplate(ctx context.Context, scope model.ProfileScope, id string) error {
	st, err := s.textStore(ctx, scope)
	if err != nil {
		return err
	}
	return st.Remove(id)
}

// IsTextTemplateNotFound / IsTextTemplateDuplicate classify store errors for
// frontends, which must not import internal/texttmpl.
func IsTextTemplateNotFound(err error) bool  { return errors.Is(err, texttmpl.ErrNotFound) }
func IsTextTemplateDuplicate(err error) bool { return errors.Is(err, texttmpl.ErrDuplicate) }

// ValidateTextTemplate checks the limits and proves the body's known tokens
// well-formed by a dry resolve (placeholder inputs, zero counters). An
// unknown <…> is literal text and passes.
func ValidateTextTemplate(title, body string) error {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return fmt.Errorf("invalid text template: the title is empty")
	case strings.ContainsAny(title, "\r\n"):
		return fmt.Errorf("invalid text template: the title must be one line")
	case utf8.RuneCountInString(title) > MaxTextTemplateTitle:
		return fmt.Errorf("invalid text template: the title is longer than %d characters", MaxTextTemplateTitle)
	case texttmpl.ID(title) == "":
		return fmt.Errorf("invalid text template: the title needs a letter or a digit")
	case strings.TrimSpace(body) == "":
		return fmt.Errorf("invalid text template: the text is empty")
	case len(body) > MaxTextTemplateBody:
		return fmt.Errorf("invalid text template: the text is larger than %d KiB", MaxTextTemplateBody>>10)
	}
	inputs := map[string]string{}
	for _, l := range template.TextTokens(body).UserLabels {
		inputs[l] = "x"
	}
	tctx := template.Ctx{
		ParentBranch: "parent", Repo: "repo", Branch: "branch",
		Seqs: map[string]int{}, Now: time.Now, Rand: rand.New(rand.NewPCG(1, 2)),
	}
	if _, err := template.ResolveText(body, inputs, tctx); err != nil {
		return fmt.Errorf("invalid text template: %w", err)
	}
	return nil
}

// TextTemplateTokens returns a body's <user:…> labels and its other tokens
// as written — what a frontend prompts for and what it lists as automatic.
// Frontends route through domain because internal/template is a layering
// detail (the PrefixUserLabels pattern).
func TextTemplateTokens(body string) (userLabels, automatic []string) {
	set := template.TextTokens(body)
	return set.UserLabels, set.Automatic
}

// TextTemplateSeqNames returns the <seq:…> counters a body consumes when its
// rendered text is taken.
func TextTemplateSeqNames(body string) []string {
	return template.TextTokens(body).SeqNames
}

// TextTemplateID is the id a title is stored under ("" when the title has no
// letter or digit) — what makes two titles the same template in a scope.
func TextTemplateID(title string) string { return texttmpl.ID(title) }

// textCtx is the resolve context for this repo: <branch> and <parent-branch>
// are the current branch ("" when detached), <repo> the main worktree's
// directory name. It also returns the git common dir the counters live in.
func (s *Service) textCtx(ctx context.Context) (template.Ctx, string) {
	branch, err := s.CurrentBranch(ctx)
	if err != nil {
		branch = ""
	}
	repo := ""
	if wts, werr := s.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		repo = worktree.RepoName(wts[0].Path)
	}
	gitDir := ""
	if cd, cerr := s.GitCommonDir(ctx); cerr == nil {
		gitDir = strings.TrimSpace(cd)
		if repo == "" {
			repo = filepath.Base(filepath.Dir(gitDir))
		}
	}
	return template.Ctx{
		ParentBranch: branch,
		Repo:         repo,
		Branch:       branch,
		Now:          clock.Now,
		Rand:         rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}, gitDir
}

// RenderTextTemplate previews body against this repo. <seq:…> counters are
// PEEKED, never consumed; the returned names are the counters to bump
// (BumpPrefixSeqs) once the rendered text is actually taken.
func (s *Service) RenderTextTemplate(ctx context.Context, body string, inputs map[string]string) (string, []string, error) {
	tctx, gitDir := s.textCtx(ctx)
	names := template.TextTokens(body).SeqNames
	tctx.Seqs = worktree.PeekSeqs(gitDir, names)
	out, err := template.ResolveText(body, inputs, tctx)
	if err != nil {
		return "", nil, err
	}
	return out, names, nil
}

// TakeTextTemplate consumes the body's <seq:…> counters and resolves with the
// numbers THIS call was handed (another process may bump between a peek and
// here — the gg prefix resolve --bump rule). A body that cannot resolve
// consumes nothing.
func (s *Service) TakeTextTemplate(ctx context.Context, body string, inputs map[string]string) (string, error) {
	tctx, gitDir := s.textCtx(ctx)
	names := template.TextTokens(body).SeqNames
	tctx.Seqs = worktree.PeekSeqs(gitDir, names)
	out, err := template.ResolveText(body, inputs, tctx)
	if err != nil || len(names) == 0 {
		return out, err
	}
	taken := map[string]int{}
	for _, n := range names {
		v, berr := config.BumpSeq(gitDir, n)
		if berr != nil {
			return "", fmt.Errorf("could not advance <seq:%s>: %w", n, berr)
		}
		taken[n] = v
	}
	tctx.Seqs = taken
	return template.ResolveText(body, inputs, tctx)
}

// FindTextTemplate resolves an id or a unique id prefix. An exact id beats a
// prefix; with scope nil the repo row wins when both scopes hold the id.
func (s *Service) FindTextTemplate(ctx context.Context, idPrefix string, scope *model.ProfileScope) (model.TextTemplate, error) {
	all, err := s.TextTemplates(ctx)
	if err != nil {
		return model.TextTemplate{}, err
	}
	var exact, pre []model.TextTemplate
	for _, t := range all {
		if scope != nil && t.Scope != *scope {
			continue
		}
		if t.ID == idPrefix {
			exact = append(exact, t)
		} else if idPrefix != "" && strings.HasPrefix(t.ID, idPrefix) {
			pre = append(pre, t)
		}
	}
	pick := exact
	if len(pick) == 0 {
		pick = pre
	}
	switch {
	case len(pick) == 0:
		return model.TextTemplate{}, fmt.Errorf("%w: %q (gg template list shows the ids)", texttmpl.ErrNotFound, idPrefix)
	case len(pick) == 1:
		return pick[0], nil
	case len(exact) > 1: // the same id in both scopes: the repo row wins
		for _, t := range exact {
			if t.Scope == model.ProfileScopeRepo {
				return t, nil
			}
		}
	}
	return model.TextTemplate{}, fmt.Errorf("%q matches %d text templates — give more of the id", idPrefix, len(pick))
}
