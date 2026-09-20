package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

const prUsage = `usage: gg pr list [--state all|open|closed|merged] [--search <text>] [--limit <n>] [--json]
       gg pr view <number> [--json]
       gg pr comments <number> [--json]
       gg pr fetch <number>
       gg pr forget <number>`

// cmdPR is the read-only pull-request surface: it lists and reads, fetches a
// PR head into the private ref refs/gg/pr/<n>, and never writes to the forge.
// Unlike the TUI — which simply hides the feature — the CLI was ASKED, so a
// missing forge CLI is an error with the detection reason.
func cmdPR(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	usage := func() int { fmt.Fprintln(stderr, prUsage); return 2 }
	// --json may sit before or after the number (agents write both). The
	// search flags take a value — the NEXT argument whatever it looks like
	// (GitHub's "-label:bug" starts with a dash), or the --flag=value form.
	asJSON, searching := false, false
	var query domain.PRQuery
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, hasVal := strings.Cut(a, "=")
		switch {
		case a == "--json" || a == "-json":
			asJSON = true
		case name == "--state" || name == "--search" || name == "--limit":
			if !hasVal {
				if i+1 >= len(args) {
					fmt.Fprintf(stderr, "error: %s needs a value\n", name)
					return usage()
				}
				i++
				val = args[i]
			}
			searching = true
			switch name {
			case "--state":
				query.State = val
			case "--search":
				query.Text = val
			case "--limit":
				n, err := strconv.Atoi(val)
				if err != nil || n < 1 {
					fmt.Fprintf(stderr, "error: --limit %q is not a positive number\n", val)
					return usage()
				}
				query.Limit = n
			}
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(stderr, "error: unknown flag %s\n", a)
			return usage()
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) == 0 {
		return usage()
	}
	verb, n := pos[0], 0
	switch verb {
	case "list":
		if len(pos) != 1 {
			return usage()
		}
		if searching {
			var err error
			if query, err = domain.NormalizePRQuery(query); err != nil {
				fmt.Fprintln(stderr, "error:", err)
				return usage()
			}
		}
	case "view", "comments", "fetch", "forget":
		if len(pos) != 2 {
			return usage()
		}
		var err error
		if n, err = strconv.Atoi(strings.TrimPrefix(pos[1], "#")); err != nil || n <= 0 {
			fmt.Fprintf(stderr, "error: %q is not a pull request number\n", pos[1])
			return usage()
		}
	default:
		return usage()
	}
	if searching && verb != "list" {
		fmt.Fprintln(stderr, "error: --state, --search and --limit belong to gg pr list")
		return usage()
	}

	ctx := context.Background()
	if st := svc.ForgeStatus(ctx); !st.Available() {
		fmt.Fprintf(stderr, "gg pr: %v\n", st.Err)
		return 1
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "error:", err); return 1 }
	emit := func(v any) int {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			return fail(err)
		}
		return 0
	}
	switch verb {
	case "list":
		// A search flag turns the listing into a forge search of any state;
		// without one it is the open + known list, as ever.
		var prs []model.PullRequest
		var err error
		more := false
		if searching {
			var res domain.PRSearchResult
			res, err = svc.PRSearch(ctx, query)
			prs, more = res.PRs, res.More
		} else {
			prs, err = svc.PullRequests(ctx)
		}
		if err != nil {
			return fail(err)
		}
		if more {
			// stderr: the rows (and --json) stay exactly the pull requests.
			fmt.Fprintln(stderr, "more results — narrow the search")
		}
		if asJSON {
			if prs == nil {
				prs = []model.PullRequest{}
			}
			return emit(prs)
		}
		if len(prs) == 0 {
			fmt.Fprintln(stdout, "(no pull requests)")
		}
		for _, p := range prs {
			fmt.Fprintln(stdout, prLine(p))
		}
	case "view":
		p, err := svc.PullRequest(ctx, n)
		if err != nil {
			return fail(err)
		}
		cs, err := svc.PRComments(ctx, n)
		if err != nil {
			return fail(err)
		}
		if asJSON {
			return emit(struct {
				PR       model.PullRequest `json:"pr"`
				Comments domain.PRComments `json:"comments"`
			}{p, cs})
		}
		printPRView(stdout, p, cs)
	case "comments":
		cs, err := svc.PRComments(ctx, n)
		if err != nil {
			return fail(err)
		}
		if asJSON {
			return emit(cs)
		}
		printPRComments(stdout, cs)
	case "fetch":
		op, err := svc.PRFetchOp(ctx, n)
		if err != nil {
			return fail(err)
		}
		if _, err := runOperation(ctx, svc, op, cliDecider{out: stderr}, stderr); err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "refs/gg/pr/"+strconv.Itoa(n))
	case "forget":
		res, err := runOperation(ctx, svc, svc.PRForgetOp(n), cliDecider{out: stderr}, stderr)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, res.Summary)
	}
	return 0
}

// prLine is the one-line PR row: "#7 open alice feat/x → main [draft] title".
// A fork head prints as owner:branch, the way forges spell it.
func prLine(p model.PullRequest) string {
	src := p.Source
	if p.SourceRepo != "" {
		src = strings.SplitN(p.SourceRepo, "/", 2)[0] + ":" + src
	}
	tags := ""
	if p.Draft {
		tags += " [draft]"
	}
	if p.ReviewState != "" {
		tags += " [" + p.ReviewState + "]"
	}
	return fmt.Sprintf("#%-4d %-11s %s  %s → %s%s  %s", p.Number, p.State, p.Author, src, p.Target, tags, p.Title)
}

func printPRView(w io.Writer, p model.PullRequest, cs domain.PRComments) {
	fmt.Fprintln(w, prLine(p))
	fmt.Fprintln(w, p.URL)
	if p.Body != "" {
		fmt.Fprintf(w, "\n%s\n", p.Body)
	}
	if len(cs.Hub) > 0 {
		fmt.Fprintln(w, "\n── conversation ──")
		for _, c := range cs.Hub {
			who := c.Author
			if c.Verdict != "" {
				who += " [" + c.Verdict + "]"
			}
			fmt.Fprintf(w, "%s: %s\n", who, hang(c.Body))
		}
	}
	if len(cs.Outdated) > 0 {
		fmt.Fprintln(w, "\n── outdated ──")
		for _, c := range cs.Outdated {
			fmt.Fprintf(w, "%s %s: %s\n", commentWhere(c), c.Author, hang(c.Body))
			if c.ParentID == "" && c.Hunk != "" {
				for _, l := range lastLines(c.Hunk, 6) {
					fmt.Fprintln(w, "    "+l)
				}
			}
		}
	}
	if cs.Truncated {
		fmt.Fprintln(w, "\n(comment list truncated — open the pull request on the forge for the rest)")
	}
}

func printPRComments(w io.Writer, cs domain.PRComments) {
	if len(cs.Inline) == 0 {
		fmt.Fprintln(w, "(no inline comments)")
	}
	for _, c := range cs.Inline {
		if c.ParentID != "" {
			fmt.Fprintf(w, "  %s: %s\n", c.Author, hang(c.Body))
			continue
		}
		who := c.Author
		if c.Resolved {
			who += " [resolved]" // beside the author: a body may run many lines
		}
		fmt.Fprintf(w, "%s %s: %s\n", commentWhere(c), who, hang(c.Body))
	}
	if cs.Truncated {
		fmt.Fprintln(w, "(comment list truncated)")
	}
}

// commentWhere is a comment's anchor label: "a.go:10-12 (new)", "a.go:7 (old)",
// "a.go (file)", or "a.go (old)" when the forge kept no line at all.
func commentWhere(c model.ForgeComment) string {
	switch {
	case c.Kind == model.ForgeCommentFile:
		return c.Path + " (file)"
	case c.Line <= 0:
		return fmt.Sprintf("%s (%s)", c.Path, c.Side)
	}
	span := strconv.Itoa(c.Line)
	if c.StartLine > 0 && c.StartLine != c.Line {
		span = strconv.Itoa(c.StartLine) + "-" + span
	}
	return fmt.Sprintf("%s:%s (%s)", c.Path, span, c.Side)
}

// hang indents a body's continuation lines so a multi-line comment cannot be
// mistaken for further rows.
func hang(body string) string { return strings.ReplaceAll(body, "\n", "\n    ") }

func lastLines(s string, n int) []string {
	ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return ls
}
