package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// cmdConfig implements `gg config <subcommand>`. Currently only `init`, which
// scaffolds a fully-commented config file. Pure file I/O — no git writes.
func cmdConfig(svc *domain.Service, workdir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg config (init | populate) (--repo | --global) [--force]")
		return 2
	}
	switch args[0] {
	case "init":
		return cmdConfigInit(svc, workdir, args[1:], stdout, stderr)
	case "populate":
		return cmdConfigPopulate(svc, workdir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown config subcommand %q\n", args[0])
		return 2
	}
}

func cmdConfigInit(svc *domain.Service, workdir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.Bool("repo", false, "write ./.gg.toml for this repository")
	global := fs.Bool("global", false, "write the global config (~/.config/gg/config.toml)")
	force := fs.Bool("force", false, "overwrite an existing file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repo == *global { // neither or both
		fmt.Fprintln(stderr, "config init: pass exactly one of --repo or --global")
		return 2
	}

	var path string
	if *repo {
		// gg reads repo config from the git toplevel, not the cwd — write there so
		// `gg config init --repo` from a subdirectory isn't a silent no-op. Fall
		// back to workdir when not inside a repo.
		root := workdir
		if top, err := svc.TopLevel(context.Background()); err == nil && top != "" {
			root = top
		}
		path = filepath.Join(root, ".gg.toml")
	} else {
		path = config.DefaultGlobalPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintf(stderr, "config init: %v\n", err)
			return 1
		}
	}

	if _, err := os.Stat(path); err == nil && !*force {
		fmt.Fprintf(stderr, "config init: %s already exists (use --force to overwrite)\n", path)
		return 1
	}
	if err := os.WriteFile(path, []byte(config.Template()), 0o644); err != nil {
		fmt.Fprintf(stderr, "config init: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "wrote", path)
	return 0
}

// cmdConfigPopulate implements `gg config populate`. It adds every supported
// setting not already present to the target file as a commented, [populated]-
// marked line, leaving existing content untouched. Pure file I/O — no git
// writes (beyond the TopLevel read for --repo).
func cmdConfigPopulate(svc *domain.Service, workdir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config populate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.Bool("repo", false, "top up ./.gg.toml for this repository")
	global := fs.Bool("global", false, "top up the global config (~/.config/gg/config.toml)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repo == *global { // neither or both
		fmt.Fprintln(stderr, "config populate: pass exactly one of --repo or --global")
		return 2
	}

	var path string
	if *repo {
		root := workdir
		if top, err := svc.TopLevel(context.Background()); err == nil && top != "" {
			root = top
		}
		path = filepath.Join(root, ".gg.toml")
	} else {
		path = config.DefaultGlobalPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintf(stderr, "config populate: %v\n", err)
			return 1
		}
	}

	added, err := config.PopulateFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "config populate: %v\n", err)
		return 1
	}
	if added == 0 {
		fmt.Fprintln(stdout, path, "already complete")
	} else {
		fmt.Fprintf(stdout, "populated %s (%d added)\n", path, added)
	}
	return 0
}
