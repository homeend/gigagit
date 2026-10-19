package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/steer"
)

const permOverviewUsage = `usage: gg overview save [<gg-link>] --agent <name> (--stdin | --file <path>) [--title <t>] [--json] [--dry-run]
       gg overview list [--all] [--json]
       gg overview show <id|gg-link> [--json]
       gg overview export [--out <path>|-] [--force] <id|gg-link>
       gg overview rm <id>`

// cmdOverview is `gg overview …`: permanent overviews (spec 2026-10-12).
func cmdOverview(svc *domain.Service, rest []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(rest) == 0 {
		fmt.Fprintln(stderr, permOverviewUsage)
		return 2
	}
	switch rest[0] {
	case "save":
		return overviewSave(svc, rest[1:], stdin, stdout, stderr)
	case "list":
		return overviewList(svc, rest[1:], stdout, stderr)
	case "show":
		return overviewShow(svc, rest[1:], stdout, stderr)
	case "export":
		return overviewExport(svc, rest[1:], stdout, stderr)
	case "rm":
		return overviewRm(svc, rest[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, permOverviewUsage)
	return 2
}

func overviewSave(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("overview save", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agent := fs.String("agent", "", "the agent's name (shown on the overview)")
	fromStdin := fs.Bool("stdin", false, "read the overview document from stdin")
	file := fs.String("file", "", "read the overview document from a file")
	title := fs.String("title", "", "the title, when the document has none")
	dry := fs.Bool("dry-run", false, "check only; store nothing")
	asJSON := fs.Bool("json", false, "print JSON")
	link := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		link, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || strings.TrimSpace(*agent) == "" || *fromStdin == (*file != "") {
		fmt.Fprintln(stderr, permOverviewUsage)
		return 2
	}
	ctx := context.Background()
	tsvc := svc
	var target domain.OverviewTarget
	var err error
	if link == "" {
		target, err = svc.HeadOverviewTarget(ctx)
	} else {
		res, rerr := resolveLinkArg(ctx, svc, link, linkShapes{Pair: true, Ref: true}, "overview save")
		if rerr != nil {
			return linkExit("overview save", rerr, stderr)
		}
		tsvc = openLinkTarget(res)
		target, err = tsvc.LinkOverviewTarget(ctx, res)
	}
	if err != nil {
		fmt.Fprintln(stderr, "overview save:", err)
		return 1
	}
	var data []byte
	if *fromStdin {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(*file)
	}
	if err != nil {
		fmt.Fprintln(stderr, "overview save:", err)
		return 1
	}
	saved, warn, err := tsvc.SaveOverview(ctx, domain.SaveOverview{Target: target, Agent: strings.TrimSpace(*agent),
		Title: *title, Text: data, DryRun: *dry})
	var refused *domain.OverviewRefused
	switch {
	case errors.As(err, &refused):
		fmt.Fprintln(stderr, "overview save: refused — nothing stored:")
		for _, p := range refused.Problems {
			fmt.Fprintln(stderr, "  "+p)
		}
		return 1
	case errors.Is(err, notebatch.ErrNotOverviewDoc):
		fmt.Fprintln(stderr, "overview save:", err)
		return 1
	case err != nil:
		fmt.Fprintln(stderr, "overview save:", err)
		return 1
	}
	if saved.Anchors == 0 && !*asJSON {
		fmt.Fprintln(stderr, noAnchorWarning)
	}
	where := short7(target.Commit)
	if target.PR != 0 {
		where = fmt.Sprintf("PR #%d @ %s", target.PR, where)
	} else if target.Branch != "" {
		where = target.Branch + " @ " + where
	}
	if *dry {
		if *asJSON {
			_ = json.NewEncoder(stdout).Encode(map[string]any{"commit": target.Commit, "branch": target.Branch, "pr": target.PR, "title": saved.Doc.Title})
		} else {
			fmt.Fprintf(stdout, "would store %q on %s\n", saved.Doc.Title, where)
		}
		return 0
	}
	steer.NotifyReload(steerDirFor(tsvc), "notes")
	text, lerr := tsvc.OverviewLink(ctx, saved.ID)
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(map[string]any{"id": saved.ID, "link": text, "commit": target.Commit,
			"branch": target.Branch, "pr": target.PR, "title": saved.Doc.Title, "replaced": saved.Replaced, "warn": warn, "anchors": saved.Anchors})
	} else {
		verb := "stored"
		if saved.Replaced {
			verb = "replaced"
		}
		fmt.Fprintf(stdout, "overview %s %s on %s: %s\n", saved.ID, verb, where, saved.Doc.Title)
		if lerr == nil {
			fmt.Fprintln(stdout, text)
		}
		if warn != "" {
			fmt.Fprintln(stderr, "warning:", warn)
		}
	}
	if lerr != nil {
		fmt.Fprintln(stderr, "warning: no link for the overview:", lerr)
	}
	return 0
}

// noAnchorWarning is what every overview write prints for a text with no
// anchor: it is stored or shown, but it opens no code.
const noAnchorWarning = "warning: the overview has no anchor — it opens no code; link the lines like [the parser](internal/x/parse.go:40-60)"

// overviewKind is a row's state word.
func overviewKind(o domain.Overview) string {
	switch {
	case o.Scope != "":
		return "pr"
	case o.Kind == domain.ReviewOnBranch:
		return "on-branch"
	case o.Kind == domain.ReviewWasTip:
		return "was-tip"
	}
	return "commit"
}

func overviewList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("overview list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("all", false, "every overview in the repository (default: HEAD's)")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(stderr, permOverviewUsage)
		return 2
	}
	ctx := context.Background()
	var ovs []domain.Overview
	var err error
	if *all {
		ovs, err = svc.Overviews(ctx)
	} else {
		head, ok, herr := svc.ResolveRev(ctx, "HEAD")
		if herr != nil || !ok {
			fmt.Fprintln(stderr, "overview list: HEAD has no commit")
			return 1
		}
		ovs, err = svc.OverviewsForCommit(ctx, strings.TrimSpace(head))
	}
	if err != nil {
		fmt.Fprintln(stderr, "overview list:", err)
		return 1
	}
	if *asJSON {
		rows := make([]map[string]any, 0, len(ovs))
		for _, o := range ovs {
			rows = append(rows, overviewListRow(o))
		}
		_ = json.NewEncoder(stdout).Encode(rows)
		return 0
	}
	for _, o := range ovs {
		fmt.Fprintf(stdout, "%s %s %s %s %s  %s\n", o.ID, short7(o.Commit), overviewKind(o),
			o.Created.Local().Format("2006-01-02 15:04"), o.Agent, o.Title)
	}
	return 0
}

// overviewListRow is one `overview list --json` row; a PR overview names
// its PR and scope.
func overviewListRow(o domain.Overview) map[string]any {
	pr, _ := domain.PRScopeNumber(o.Scope)
	return map[string]any{"id": o.ID, "commit": o.Commit, "branch": o.Branch, "kind": overviewKind(o),
		"pr": pr, "scope": o.Scope, "agent": o.Agent, "title": o.Title, "created": o.Created}
}

// overviewShowJSON is `overview show --json`; a stored document that no
// longer parses exits 1, as the text form does.
func overviewShowJSON(stdout, stderr io.Writer, o domain.Overview, link string) int {
	var doc json.RawMessage
	row := map[string]any{"id": o.ID, "link": link, "commit": o.Commit, "branch": o.Branch,
		"kind": overviewKind(o), "agent": o.Agent, "title": o.Title}
	if o.Doc != nil {
		doc = o.Doc.Canonical()
	} else {
		row["warn"] = "the stored document no longer parses"
	}
	row["document"] = doc
	_ = json.NewEncoder(stdout).Encode(row)
	if o.Doc == nil {
		fmt.Fprintln(stderr, "warning: the stored document no longer parses")
		return 1
	}
	return 0
}

// overviewByArg reads an id or an overview link.
func overviewByArg(ctx context.Context, svc *domain.Service, arg, verb string) (*domain.Service, domain.Overview, error) {
	if strings.HasPrefix(arg, "gg://") {
		res, err := resolveLinkArg(ctx, svc, arg, linkShapes{}, verb)
		if err != nil {
			return nil, domain.Overview{}, err
		}
		if res.Hint.Kind != model.OverviewHintKind {
			return nil, domain.Overview{}, errors.New("that link names no overview (it has no ?overview=<id>)")
		}
		tsvc := openLinkTarget(res)
		o, err := tsvc.Overview(ctx, res.Hint.ID)
		return tsvc, o, err
	}
	o, err := svc.Overview(ctx, arg)
	return svc, o, err
}

// overviewExport is `gg overview export`: the overview as one self-contained
// HTML file (spec 2026-10-12-overview-html-export) — the files at its
// commit, their diffs as the split's D shows them, no remarks.
func overviewExport(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("overview export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "write here (default: the export dir); - = stdout")
	force := fs.Bool("force", false, "overwrite an existing file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, permOverviewUsage)
		return 2
	}
	ctx := context.Background()
	tsvc, id := svc, fs.Arg(0)
	if strings.HasPrefix(id, "gg://") { // a link names its checkout; an id is read once, by the export
		lsvc, o, err := overviewByArg(ctx, svc, id, "overview export")
		if err != nil {
			fmt.Fprintln(stderr, "overview export:", err)
			return 1
		}
		tsvc, id = lsvc, o.ID
	}
	page, name, err := tsvc.ExportOverviewHTML(ctx, domain.OverviewSource{OverviewID: id})
	if err != nil {
		fmt.Fprintln(stderr, "overview export:", err)
		return 1
	}
	return writeExport(ctx, tsvc, "overview export", page, name, *out, *force, stdout, stderr)
}

func overviewShow(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	asJSON := false
	var pos []string
	for _, a := range args {
		if a == "--json" {
			asJSON = true
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, permOverviewUsage)
		return 2
	}
	ctx := context.Background()
	tsvc, o, err := overviewByArg(ctx, svc, pos[0], "overview show")
	if err != nil {
		fmt.Fprintln(stderr, "overview show:", err)
		return 1
	}
	link, _ := tsvc.OverviewLink(ctx, o.ID)
	if asJSON {
		return overviewShowJSON(stdout, stderr, o, link)
	}
	fmt.Fprintf(stdout, "%s — %s (%s %s, %s)\n", o.ID, o.Title, overviewKind(o), short7(o.Commit), o.Agent)
	if link != "" {
		fmt.Fprintln(stdout, link)
	}
	if o.Doc == nil {
		fmt.Fprintln(stderr, "warning: the stored document no longer parses")
		return 1
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, strings.TrimRight(o.Doc.Overview, "\n"))
	for _, f := range o.Doc.Files {
		for _, n := range f.Notes {
			fmt.Fprintf(stdout, "\n%s:%s  %s\n", f.Path, rangeText(n.Range), n.Summary)
			if r := strings.TrimSpace(n.Rationale); r != "" {
				fmt.Fprintln(stdout, "    "+strings.ReplaceAll(r, "\n", "\n    "))
			}
		}
	}
	return 0
}

func rangeText(r [2]int) string {
	if r[1] > r[0] {
		return fmt.Sprintf("%d-%d", r[0], r[1])
	}
	return fmt.Sprint(r[0])
}

func overviewRm(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, permOverviewUsage)
		return 2
	}
	if err := svc.RemoveOverview(context.Background(), args[0]); err != nil {
		fmt.Fprintln(stderr, "overview rm:", err)
		return 1
	}
	steer.NotifyReload(steerDirFor(svc), "notes")
	fmt.Fprintln(stdout, "removed overview", args[0])
	return 0
}
