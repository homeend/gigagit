package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdCommitExportPatch implements `gg commit export-patch <sha> [--out <path>]
// [--force] [-- <file>]`. Without a `-- <file>` it exports the whole commit's
// patch; with one it exports just that file's change within the commit. Merge
// commits are refused (domain.ErrMergeCommitPatch).
//
// Parsing is manual and order-independent (the <sha> positional may precede or
// follow --out/--force), mirroring cmdCommitReword: Go's flag.FlagSet stops
// parsing at the first non-flag argument, which would break `<sha> --out <path>`.
func cmdCommitExportPatch(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	const usage = "usage: gg commit export-patch <sha> [--out <path>] [--force] [-- <file>]"
	sha := ""
	out := ""
	force := false
	file := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			// Everything after `--` is the optional single file scope.
			if i+1 < len(args) {
				file = args[i+1]
			}
			if i+2 < len(args) {
				fmt.Fprintln(stderr, "commit export-patch: too many arguments after --")
				return 2
			}
			i = len(args)
		case a == "--out":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			out = args[i+1]
			i++
		case a == "--force":
			force = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(stderr, "commit export-patch: unknown flag %q\n", a)
			return 2
		default:
			if sha != "" {
				fmt.Fprintln(stderr, "commit export-patch: too many arguments (expected one <sha>)")
				return 2
			}
			sha = a
		}
	}
	if sha == "" {
		fmt.Fprintln(stderr, usage)
		return 2
	}

	ctx := context.Background()
	var (
		data []byte
		name string
		err  error
	)
	if file == "" {
		data, name, err = svc.CommitPatch(ctx, sha)
	} else {
		data, name, err = svc.FilePatch(ctx, sha, file)
	}
	if err != nil {
		fmt.Fprintf(stderr, "commit export-patch: %v\n", err)
		return 1
	}

	target := out
	if target == "" {
		dir, derr := svc.ExportDefaultDir(ctx)
		if derr != nil {
			fmt.Fprintf(stderr, "commit export-patch: %v\n", derr)
			return 1
		}
		target = filepath.Join(dir, name)
	}

	// Answer the engine's Overwrite/Cancel fork from policy only: --force =
	// overwrite, otherwise cancel (an existing file then refuses).
	policy := map[string]string{"overwrite": "cancel"}
	if force {
		policy["overwrite"] = "overwrite"
	}
	dec := cliDecider{policy: policy}
	res, err := runOperation(ctx, svc, engine.ExportFile{Path: target, Data: data}, dec, stderr)
	if errors.Is(err, engine.ErrWriteCancelled) {
		fmt.Fprintf(stderr, "commit export-patch: %s already exists; pass --force to overwrite\n", target)
		return 2
	}
	return finish(res, err, stdout, stderr)
}
