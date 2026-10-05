package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// `gg review show` reads a stored review back — the agent half of a review
// link: agent A's review, pasted to agent B as gg://…?review=<id>, prints its
// overview and every remark with the remark's own line link.

const reviewShowUsage = "usage: gg review show [--json] <review-link|id|latest>"

func reviewShow(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the review as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, reviewShowUsage)
		return 2
	}
	ctx := context.Background()
	id, code := reviewArgID(ctx, svc, fs.Arg(0), stderr)
	if code != 0 {
		return code
	}
	rs, err := svc.ReviewShow(ctx, id)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(rs); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	printReviewShow(stdout, rs)
	return 0
}

// reviewArgID turns a review link, an id or "latest" into a stored review's
// id. Exit 2 for a link that is malformed or names no review, 1 for a review
// that is not here (or a link moved onto another change).
func reviewArgID(ctx context.Context, svc *domain.Service, arg string, stderr io.Writer) (string, int) {
	if strings.HasPrefix(arg, "gg://") {
		l, err := model.ParseLink(arg)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return "", 2
		}
		if l.Hint.Kind != model.ReviewHintKind {
			fmt.Fprintln(stderr, "error: that link names no review (no ?review=<id>)")
			return "", 2
		}
		if _, err := resolveLinkArg(ctx, svc, arg, linkShapes{Pair: true, Ref: true}, "review show"); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			if errors.Is(err, model.ErrLink) && !errors.Is(err, domain.ErrReviewLinkMismatch) {
				return "", 2
			}
			return "", 1
		}
		arg = l.Hint.ID
	}
	id, err := svc.ReviewID(ctx, arg)
	if err != nil {
		if errors.Is(err, domain.ErrReviewNotFound) {
			fmt.Fprintf(stderr, "error: review %s not found\n", arg)
		} else {
			fmt.Fprintln(stderr, "error:", err)
		}
		return "", 1
	}
	return id, 0
}

func printReviewShow(w io.Writer, rs domain.ReviewShow) {
	what := short7(rs.Tip)
	if rs.Base != "" {
		what = short7(rs.Base) + ".." + short7(rs.Tip)
	}
	if rs.Branch != "" {
		what += " (" + rs.Branch + ")"
	}
	fmt.Fprintf(w, "review %s · %s · %s · %s\n", rs.ID, rs.Agent, rs.Created.Local().Format("2006-01-02 15:04"), what)
	fmt.Fprintln(w, strings.TrimRight(rs.Overview, "\n"))
	if len(rs.Meta) > 0 {
		fmt.Fprintf(w, "\n%s\n", metaMapText(rs.Meta))
	}
	if len(rs.Remarks) > 0 {
		fmt.Fprintln(w)
	}
	for _, r := range rs.Remarks {
		line := fmt.Sprint(r.Start)
		if r.End > r.Start {
			line += fmt.Sprintf("-%d", r.End)
		}
		if r.Side == string(model.NoteSideOld) {
			line = "-" + line
		}
		fmt.Fprintf(w, "[%d] %s:%s — %s", r.N, r.Path, line, r.Summary)
		if len(r.Meta) > 0 {
			fmt.Fprintf(w, " (%s)", metaMapText(r.Meta))
		}
		fmt.Fprintln(w)
		if t := strings.TrimSpace(r.Rationale); t != "" {
			fmt.Fprintln(w, "    "+strings.ReplaceAll(t, "\n", "\n    "))
		}
		if r.Link != "" {
			fmt.Fprintln(w, "    "+r.Link)
		}
	}
}

// metaMapText is metaText for a map: "key: value" pairs, sorted by key.
func metaMapText(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + m[k]
	}
	return strings.Join(parts, ", ")
}

func short7(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// linkReview is `gg link --review <id|latest>`: the review's link, recorded
// in the copied-link history like every printed link.
func linkReview(svc *domain.Service, idOrLatest string, stdout, stderr io.Writer) int {
	ctx := context.Background()
	id, err := svc.ReviewID(ctx, idOrLatest)
	if err != nil {
		if errors.Is(err, domain.ErrReviewNotFound) {
			fmt.Fprintf(stderr, "error: review %s not found (ids: gg review prints one; gg note list shows them)\n", idOrLatest)
		} else {
			fmt.Fprintln(stderr, "error:", err)
		}
		return 1
	}
	text, err := svc.ReviewLink(ctx, id)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	svc.RecordCopiedLink(ctx, text)
	fmt.Fprintln(stdout, text)
	return 0
}
