package cli

// `gg skill path [review|using-gg]` materialises an embedded skill in the user
// cache dir and prints its absolute path — so an agent can read gg's skill
// without `gg init` ever having run in this repository.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/agentskill"
)

// SkillCacheDir overrides the base directory `gg skill path` writes into. ""
// uses os.UserCacheDir(). Tests set it: UserCacheDir ignores XDG_CACHE_HOME on
// macOS and Windows, so an env var alone cannot isolate this cross-platform.
var SkillCacheDir string

// skillUsage is what every caller mistake — and -h/--help — prints. It names
// the default and the cache's refresh rule, because both decide whether an
// agent gets the skill this binary carries or a stale one.
const skillUsage = `usage: gg skill path [review|using-gg]

Writes the embedded skill under the user cache dir and prints its absolute
path. The default is review (reviewing-with-gg); using-gg is the git CLI
skill. A cached copy whose marker names a different version is rewritten to
this binary's version, so the path always holds the current skill.`

// cmdSkill implements `gg skill path [review|using-gg]`.
func cmdSkill(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "path" {
		fmt.Fprintln(stderr, skillUsage)
		return 2
	}
	fs := flag.NewFlagSet("skill path", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // -h prints skillUsage below, not an empty flag dump
	if err := fs.Parse(args[1:]); err != nil {
		// -h/--help arrives here as flag.ErrHelp: print the usage and exit 2,
		// like every sibling verb's -h. A real bad flag names itself first.
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stderr, "skill:", err)
		}
		fmt.Fprintln(stderr, skillUsage)
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, skillUsage)
		return 2
	}
	name := "review"
	if fs.NArg() == 1 {
		name = fs.Arg(0)
	}
	var sk agentskill.Skill
	switch name {
	case "review", "reviewing-with-gg":
		sk = agentskill.ReviewingWithGG
	case "using-gg", "using":
		sk = agentskill.UsingGG
	default:
		fmt.Fprintf(stderr, "skill: unknown skill %q (use review or using-gg)\n", name)
		fmt.Fprintln(stderr, skillUsage)
		return 2
	}
	base := SkillCacheDir
	if base == "" {
		d, err := os.UserCacheDir()
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		base = d
	}
	path := filepath.Join(base, "gg", "skills", sk.Name, "SKILL.md")
	// Rewrite only when the file is missing or its marker names another
	// version: a materialised skill is a cache, not user-owned content.
	if data, err := os.ReadFile(path); err != nil || sk.InstalledVersion(data) != sk.Version {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if err := os.WriteFile(path, []byte(sk.SkillFile()), 0o644); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	}
	fmt.Fprintln(stdout, path)
	return 0
}
