package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// ConfigScope selects which git config layer a read/write targets.
type ConfigScope int

const (
	ConfigEffective ConfigScope = iota // merged (no --local/--global flag)
	ConfigLocal                        // repo .git/config
	ConfigGlobal                       // ~/.gitconfig
)

func (s ConfigScope) flag() (string, bool) {
	switch s {
	case ConfigLocal:
		return "--local", true
	case ConfigGlobal:
		return "--global", true
	default:
		return "", false
	}
}

// ConfigGet reads one config key at the given scope. A key that is unset
// returns ("", false, nil): `git config --get` exits 1 for a missing key,
// which the runner surfaces as a non-nil err with res.ExitCode == 1 (the
// CommitExists pattern), not a real error. Use an explicit Local/Global scope
// to distinguish a repo value from an inherited global one; ConfigEffective
// returns the merged value.
func (r *Repo) ConfigGet(ctx context.Context, scope ConfigScope, key string) (string, bool, error) {
	b := gitcmd.New("config")
	if f, ok := scope.flag(); ok {
		b = b.Arg(f)
	}
	b = b.Arg("--get", key)
	res, err := r.Runner.Run(ctx, "git config", b.ToArgv())
	if err == nil {
		return strings.TrimSpace(res.Stdout), true, nil
	}
	if res.ExitCode == 1 {
		return "", false, nil // key unset
	}
	return "", false, err
}

// ConfigSet writes one config key at the given scope (Local or Global only;
// an Effective/unknown scope falls back to Local).
func (r *Repo) ConfigSet(ctx context.Context, scope ConfigScope, key, value string) error {
	f, ok := scope.flag()
	if !ok {
		f = "--local"
	}
	b := gitcmd.New("config").Arg(f, key, value)
	_, err := r.Runner.Run(ctx, "git config", b.ToArgv())
	return err
}
