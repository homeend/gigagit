package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
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

// ConfigUnset removes one config key at the given scope (Local or Global
// only; an Effective/unknown scope falls back to Local). Uses --unset-all:
// plain --unset on a key with MULTIPLE values (a multivar, e.g.
// safe.directory) exits 5 without removing anything, which would map to a
// false "unset" success below — --unset-all removes every value of the key
// (a multivar key must not survive an "unset" that claims success), so exit
// 5 now unambiguously means "key was not set" — treated as a no-op success
// (the ConfigGet exit-1 pattern), so unset is idempotent for callers.
func (r *Repo) ConfigUnset(ctx context.Context, scope ConfigScope, key string) error {
	f, ok := scope.flag()
	if !ok {
		f = "--local"
	}
	b := gitcmd.New("config").Arg(f, "--unset-all", key)
	res, err := r.Runner.Run(ctx, "git config", b.ToArgv())
	if err != nil && res.ExitCode == 5 {
		return nil // key was not set: already in the desired state
	}
	return err
}

// ConfigUnsetValue removes the one value of a (possibly multi-valued) key
// that equals value exactly (`git config --unset --fixed-value <key>
// <value>`), at the given scope (Local or Global only; an Effective/unknown
// scope falls back to Local). Sibling values of a multivar survive — removing
// one stale fetch refspec must not clobber the rest of the list. Exit 5 means
// no value matched: a no-op success, so unset stays idempotent (the
// ConfigUnset convention).
func (r *Repo) ConfigUnsetValue(ctx context.Context, scope ConfigScope, key, value string) error {
	f, ok := scope.flag()
	if !ok {
		f = "--local"
	}
	b := gitcmd.New("config").Arg(f, "--unset", "--fixed-value", key, value)
	res, err := r.Runner.Run(ctx, "git config", b.ToArgv())
	if err != nil && res.ExitCode == 5 {
		return nil // no value matched: already in the desired state
	}
	return err
}

// ConfigAdd appends one value to a (possibly multi-valued) config key at the
// given scope (Local or Global only; an Effective/unknown scope falls back to
// Local). `git config --add` never replaces existing values — the fetch-
// refspec mapping use case must not clobber a user's refspec list.
func (r *Repo) ConfigAdd(ctx context.Context, scope ConfigScope, key, value string) error {
	f, ok := scope.flag()
	if !ok {
		f = "--local"
	}
	b := gitcmd.New("config").Arg(f, "--add", key, value)
	_, err := r.Runner.Run(ctx, "git config --add", b.ToArgv())
	return err
}

// ConfigGetAll reads every value of a (possibly multi-valued) key at the
// effective scope, in definition order. A key that is unset returns
// (nil, nil): `git config --get-all` exits 1 for a missing key (the
// ConfigGet exit-1 pattern).
func (r *Repo) ConfigGetAll(ctx context.Context, key string) ([]string, error) {
	b := gitcmd.New("config").Arg("--get-all", key)
	res, err := r.Runner.Run(ctx, "git config --get-all", b.ToArgv())
	if err != nil {
		if res.ExitCode == 1 {
			return nil, nil // key unset
		}
		return nil, err
	}
	var out []string
	for _, ln := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		if ln != "" {
			out = append(out, ln)
		}
	}
	return out, nil
}

// ConfigGetRegexp lists every effective-scope key matching pattern as
// [key, value] pairs. -z framing (records NUL-terminated, key separated
// from value by a newline) survives multi-line values — the
// ConfigListScoped precedent. No match returns (nil, nil): exit 1, the
// ConfigGet pattern.
func (r *Repo) ConfigGetRegexp(ctx context.Context, pattern string) ([][2]string, error) {
	b := gitcmd.New("config").Arg("-z", "--get-regexp", pattern)
	res, err := r.Runner.Run(ctx, "git config --get-regexp", b.ToArgv())
	if err != nil {
		if res.ExitCode == 1 {
			return nil, nil // no key matched
		}
		return nil, err
	}
	var out [][2]string
	for _, rec := range strings.Split(res.Stdout, "\x00") {
		if rec == "" {
			continue
		}
		key, val, _ := strings.Cut(rec, "\n")
		out = append(out, [2]string{key, val})
	}
	return out, nil
}
