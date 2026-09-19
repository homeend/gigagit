// Package forge is gg's read-only window onto a code forge's pull requests.
// Everything above Provider is forge-neutral; gh.go is the only file that
// knows GitHub. Owned by domain — frontends never import it.
package forge

import (
	"context"
	"errors"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// CallTimeout bounds every provider subprocess.
const CallTimeout = 30 * time.Second

// ErrNotFound: the forge has no such pull request (deleted, or never existed).
var ErrNotFound = errors.New("forge: pull request not found")

// Provider reads one forge. Implementations never mutate the forge.
type Provider interface {
	Name() string
	// Detect returns nil only when the tool is installed, authenticated and
	// able to read THIS repository's pull requests.
	Detect(ctx context.Context) error
	ListOpen(ctx context.Context) ([]model.PullRequest, error)
	PR(ctx context.Context, n int) (model.PullRequest, error)
	Comments(ctx context.Context, n int) (cs []model.ForgeComment, truncated bool, err error)
	// BaseRepo names the repository PRs target: its RepoSlug and a clone URL
	// to fall back on when no configured remote matches.
	BaseRepo(ctx context.Context) (slug, fallbackURL string, err error)
	// HeadRefspec is the server-side ref holding PR n's head.
	HeadRefspec(n int) string
}
