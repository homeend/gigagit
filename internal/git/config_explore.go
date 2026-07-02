package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ConfigKeys returns git's own config-key catalog (`git help -c`): one key
// per line, camelCase, catalog order preserved (it is already sorted).
// Placeholder keys like branch.<name>.remote come through verbatim.
func (r *Repo) ConfigKeys(ctx context.Context) ([]string, error) {
	res, err := r.Runner.Run(ctx, "git help -c", gitcmd.New("help").Arg("-c").ToArgv())
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			keys = append(keys, line)
		}
	}
	return keys, nil
}

// ConfigSetting is one set config entry with the scope it was set at.
type ConfigSetting struct {
	Scope ConfigScope
	Key   string // lowercased by git (section+key; subsections keep case)
	Value string
}

// ConfigListScoped lists every set config value with its scope
// (`git config --list --show-scope -z`). The -z record shape is
// `scope NUL key \n value NUL` — NUL-separated tokens arrive in PAIRS, and
// only -z survives multiline values. Only local and global records are kept:
// the explorer's columns are local | global | default, so system/worktree/
// command scopes are out of scope by design.
func (r *Repo) ConfigListScoped(ctx context.Context) ([]ConfigSetting, error) {
	argv := gitcmd.New("config").Arg("--list", "--show-scope", "-z").ToArgv()
	res, err := r.Runner.Run(ctx, "git config list", argv)
	if err != nil {
		return nil, err
	}
	tokens := strings.Split(res.Stdout, "\x00")
	var out []ConfigSetting
	for i := 0; i+1 < len(tokens); i += 2 {
		scope := tokens[i]
		kv := tokens[i+1]
		var sc ConfigScope
		switch scope {
		case "local":
			sc = ConfigLocal
		case "global":
			sc = ConfigGlobal
		default:
			continue // system/worktree/command: not an explorer column
		}
		key, value, _ := strings.Cut(kv, "\n")
		out = append(out, ConfigSetting{Scope: sc, Key: key, Value: value})
	}
	return out, nil
}
