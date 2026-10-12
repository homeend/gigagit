package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

const reviewExportUsage = "usage: gg review export [--out <path>|-] [--force] <review-link|id|latest>"

// reviewExport writes a stored review's overview as one self-contained HTML
// file (spec 2026-10-12-overview-html-export): the overview, its anchored
// files at the tip and their diffs, no notes. Exit 1 when the review has no
// overview, 2 on misuse or an existing file without --force.
func reviewExport(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "write here (default: the export dir); - = stdout")
	force := fs.Bool("force", false, "overwrite an existing file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, reviewExportUsage)
		return 2
	}
	ctx := context.Background()
	svc, id, _, code := reviewArgID(ctx, svc, fs.Arg(0), stderr)
	if code != 0 {
		return code
	}
	page, name, err := svc.ExportOverviewHTML(ctx, domain.OverviewSource{ReviewID: id})
	if errors.Is(err, domain.ErrNoOverview) {
		fmt.Fprintf(stderr, "review %s has no overview\n", id)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return writeExport(ctx, svc, "review export", page, name, *out, *force, stdout, stderr)
}

// writeExport writes page to out ("-" = stdout; "" = <export dir>/<name>)
// through engine.ExportFile: an existing file with other bytes needs force.
func writeExport(ctx context.Context, svc *domain.Service, verb string, page []byte, name, out string, force bool, stdout, stderr io.Writer) int {
	if out == "-" {
		_, _ = stdout.Write(page)
		return 0
	}
	target := out
	if target == "" {
		dir, err := svc.ExportDefaultDir(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", verb, err)
			return 1
		}
		target = filepath.Join(dir, name)
	}
	if abs, err := filepath.Abs(target); err == nil {
		target = abs
	}
	policy := map[string]string{"overwrite": "cancel"}
	if force {
		policy["overwrite"] = "overwrite"
	}
	res, err := runOperation(ctx, svc, engine.ExportFile{Path: target, Data: page}, cliDecider{policy: policy}, stderr)
	if errors.Is(err, engine.ErrWriteCancelled) {
		fmt.Fprintf(stderr, "%s: %s already exists; pass --force to overwrite\n", verb, target)
		return 2
	}
	return finish(res, err, stdout, stderr)
}
