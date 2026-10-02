package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

const templateUsage = "usage: gg template <list|show|render|add|edit|rm> ..."

// cmdTemplate implements `gg template …`: the two-scope registry of text
// templates (titled multi-line texts with <…> tokens) and their rendering.
func cmdTemplate(svc *domain.Service, workdir string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, templateUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list", "ls":
		return templateList(svc, rest, stdout, stderr)
	case "show":
		return templateShow(svc, rest, stdout, stderr)
	case "render":
		return templateRender(svc, rest, stdout, stderr)
	case "add":
		return templateAdd(svc, workdir, rest, stdin, stdout, stderr)
	case "edit":
		return templateEdit(svc, workdir, rest, stdin, stdout, stderr)
	case "rm", "remove":
		return templateRemove(svc, rest, stderr)
	default:
		fmt.Fprintf(stderr, "template: unknown subcommand %q\n%s\n", sub, templateUsage)
		return 2
	}
}

// parseInterleaved parses fs over args, letting flags follow positionals
// (the prefix resolve loop), and returns the positionals.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, bool) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, false
		}
		if fs.NArg() == 0 {
			return pos, true
		}
		pos, args = append(pos, fs.Arg(0)), fs.Args()[1:]
	}
}

// templateScope maps --global to an explicit scope; absent = nil (any scope,
// the repo row winning a tie).
func templateScope(global bool) *model.ProfileScope {
	if !global {
		return nil
	}
	g := model.ProfileScopeGlobal
	return &g
}

// findTemplate resolves the one positional id (or unique id prefix). An
// unknown or ambiguous id is the caller's mistake: exit 2.
func findTemplate(svc *domain.Service, verb, id string, global bool, stderr io.Writer) (model.TextTemplate, bool) {
	t, err := svc.FindTextTemplate(context.Background(), id, templateScope(global))
	if err != nil {
		msg := strings.TrimPrefix(err.Error(), "text template: ")
		if domain.IsTextTemplateNotFound(err) {
			msg += " (gg template list shows the ids)"
		}
		fmt.Fprintf(stderr, "template %s: %s\n", verb, msg)
		return model.TextTemplate{}, false
	}
	return t, true
}

// readBody reads -F's target: a file path (a relative one is taken from
// workdir, the directory the command runs against), or stdin for "-". It
// reads one byte past the largest text a template may hold and no further:
// more than that is refused by validation whatever follows.
func readBody(workdir, from string, stdin io.Reader) (string, error) {
	src := stdin
	if from != "-" {
		if !filepath.IsAbs(from) {
			from = filepath.Join(workdir, from)
		}
		f, err := os.Open(from)
		if err != nil {
			return "", err
		}
		defer f.Close()
		src = f
	}
	b, err := io.ReadAll(io.LimitReader(src, domain.MaxTextTemplateBody+1))
	return string(b), err
}

func templateList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "usage: gg template list")
		return 2
	}
	ts, err := svc.TextTemplates(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, t := range ts {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", t.ID, t.Scope.String(), t.Title)
	}
	return 0
}

func templateShow(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("template show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	global := fs.Bool("global", false, "look in the global scope only")
	pos, ok := parseInterleaved(fs, args)
	if !ok || len(pos) != 1 {
		fmt.Fprintln(stderr, "usage: gg template show <id> [--global]")
		return 2
	}
	t, ok := findTemplate(svc, "show", pos[0], *global, stderr)
	if !ok {
		return 2
	}
	fmt.Fprintln(stdout, t.Body)
	labels, auto := domain.TextTemplateTokens(t.Body)
	if len(labels)+len(auto) > 0 {
		fmt.Fprintln(stdout)
	}
	if len(labels) > 0 {
		fmt.Fprintln(stdout, "variables: "+strings.Join(labels, ", "))
	}
	if len(auto) > 0 {
		fmt.Fprintln(stdout, "automatic: "+strings.Join(auto, ", "))
	}
	return 0
}

// templateRender prints a template resolved against this repo. It CONSUMES
// the template's <seq:…> counters (the text is being taken) unless --peek.
func templateRender(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("template render", flag.ContinueOnError)
	fs.SetOutput(stderr)
	inputs := labelValues{}
	fs.Var(inputs, "set", "fill a <user:LABEL>: label=value (repeatable)")
	global := fs.Bool("global", false, "look in the global scope only")
	peek := fs.Bool("peek", false, "preview: do not advance the template's <seq> counters")
	pos, ok := parseInterleaved(fs, args)
	if !ok || len(pos) != 1 {
		fmt.Fprintln(stderr, "usage: gg template render <id> [--set label=value]... [--peek] [--global]")
		return 2
	}
	t, ok := findTemplate(svc, "render", pos[0], *global, stderr)
	if !ok {
		return 2
	}
	labels, _ := domain.TextTemplateTokens(t.Body)
	var missing []string
	for _, l := range labels {
		if _, ok := inputs[l]; !ok {
			missing = append(missing, "--set "+l+"=…")
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "template render: %s needs %s\n", t.ID, strings.Join(missing, " "))
		return 2
	}
	var unknown []string
	for l := range inputs {
		if !slices.Contains(labels, l) {
			unknown = append(unknown, l)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		asks := "no variables"
		if len(labels) > 0 {
			asks = strings.Join(labels, ", ")
		}
		fmt.Fprintf(stderr, "template render: %s has no variable %s (it asks for: %s)\n", t.ID, strings.Join(unknown, ", "), asks)
		return 2
	}
	ctx := context.Background()
	var text string
	var err error
	if *peek {
		text, _, err = svc.RenderTextTemplate(ctx, t.Body, inputs)
	} else {
		text, err = svc.TakeTextTemplate(ctx, t.Body, inputs)
	}
	if err != nil {
		fmt.Fprintln(stderr, "template render:", err)
		return 1
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	fmt.Fprint(stdout, text)
	return 0
}

func templateAdd(svc *domain.Service, workdir string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("template add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "the template's title")
	from := fs.String("F", "", "read the text from this file (- = stdin)")
	repo := fs.Bool("repo", false, "store for this repo only")
	global := fs.Bool("global", false, "store in the global (every-repo) scope — the default")
	pos, ok := parseInterleaved(fs, args)
	if !ok || len(pos) != 0 || *title == "" || *from == "" || (*repo && *global) {
		fmt.Fprintln(stderr, "usage: gg template add --title <title> -F <file|-> [--repo]")
		return 2
	}
	body, err := readBody(workdir, *from, stdin)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	// Global unless --repo: the default the TUI window and the web view use.
	scope := model.ProfileScopeGlobal
	if *repo {
		scope = model.ProfileScopeRepo
	}
	stored, err := svc.AddTextTemplate(context.Background(), model.TextTemplate{Title: *title, Body: body, Scope: scope})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, stored.ID)
	return 0
}

func templateEdit(svc *domain.Service, workdir string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("template edit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "a new title (the id follows it)")
	from := fs.String("F", "", "read the new text from this file (- = stdin)")
	global := fs.Bool("global", false, "look in the global scope only")
	pos, ok := parseInterleaved(fs, args)
	if !ok || len(pos) != 1 || (*title == "" && *from == "") {
		fmt.Fprintln(stderr, "usage: gg template edit <id> [--title <title>] [-F <file|->] [--global]")
		return 2
	}
	t, ok := findTemplate(svc, "edit", pos[0], *global, stderr)
	if !ok {
		return 2
	}
	next := model.TextTemplate{Title: t.Title, Body: t.Body}
	if *title != "" {
		next.Title = *title
	}
	if *from != "" {
		body, err := readBody(workdir, *from, stdin)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		next.Body = body
	}
	stored, err := svc.UpdateTextTemplate(context.Background(), t.Scope, t.ID, next)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, stored.ID)
	return 0
}

func templateRemove(svc *domain.Service, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("template rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	global := fs.Bool("global", false, "look in the global scope only")
	pos, ok := parseInterleaved(fs, args)
	if !ok || len(pos) != 1 {
		fmt.Fprintln(stderr, "usage: gg template rm <id> [--global]")
		return 2
	}
	t, ok := findTemplate(svc, "rm", pos[0], *global, stderr)
	if !ok {
		return 2
	}
	if err := svc.RemoveTextTemplate(context.Background(), t.Scope, t.ID); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
