package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// gg overview show <link> for a SESSION overview or a REVIEW's overview, and
// the anchor list every kind prints (spec 2026-10-28-overview-links-design):
// each anchor as a gg:// link into the overview's own checkout, so another
// agent reads the very lines with gg link text, never its own copy.

// wireAnchor is one anchor of `overview show`: a file anchor's link, or the
// note a note anchor names.
type wireAnchor struct {
	Label string          `json:"label"`
	Path  string          `json:"path,omitempty"`
	Start int             `json:"start,omitempty"`
	End   int             `json:"end,omitempty"`
	Link  string          `json:"link,omitempty"`
	Note  *wireAnchorNote `json:"note,omitempty"`
}

type wireAnchorNote struct {
	ID        string `json:"id"`
	Path      string `json:"path,omitempty"`
	Start     int    `json:"start,omitempty"`
	End       int    `json:"end,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Rationale string `json:"rationale,omitempty"`
}

// anchorRows lays anchors out against repo at commit ("" = the working
// tree); note looks a note anchor's note up (nil: a stored overview, which
// has none).
func anchorRows(anchors []agentdocs.Anchor, repo model.LinkRepo, commit string, note func(id string) (agentdocs.Note, bool)) []wireAnchor {
	out := make([]wireAnchor, 0, len(anchors))
	for _, a := range anchors {
		label := a.Label
		if label == "" {
			label = a.Dest
		}
		w := wireAnchor{Label: label, Path: a.Path, Start: a.Start, End: a.End}
		switch {
		case a.Note != "":
			w.Note = &wireAnchorNote{ID: a.Note}
			if note != nil {
				if n, ok := note(a.Note); ok {
					w.Note = &wireAnchorNote{ID: n.ID, Path: n.Path, Start: n.Start, End: n.End, Summary: n.Summary, Rationale: n.Rationale}
				}
			}
		case a.Path != "":
			w.Link, _ = domain.AnchorLink(repo, commit, a)
		}
		out = append(out, w)
	}
	return out
}

// anchorRepo is the repository anchor links name; ok false (after a
// warning on stderr) when none can be named — the anchors then print as
// bare paths rather than as links naming no checkout.
func anchorRepo(ctx context.Context, tsvc *domain.Service, stderr io.Writer) (model.LinkRepo, bool) {
	repo, err := tsvc.LinkRepo(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "warning: anchors carry no links:", err)
		return model.LinkRepo{}, false
	}
	return repo, true
}

// unlinked drops every anchor's link: there is nothing true to link to.
func unlinked(rows []wireAnchor) []wireAnchor {
	for i := range rows {
		rows[i].Link = ""
	}
	return rows
}

// printAnchors is the text form's "anchors:" section ("" for none).
func printAnchors(w io.Writer, rows []wireAnchor) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, "\nanchors:")
	for _, a := range rows {
		switch {
		case a.Note != nil && a.Note.Path == "":
			fmt.Fprintf(w, "  %s → note %s (gone)\n", a.Label, a.Note.ID)
		case a.Note != nil:
			fmt.Fprintf(w, "  %s → note %s %s:%s  %s\n", a.Label, a.Note.ID, a.Note.Path, rangeText([2]int{a.Note.Start, a.Note.End}), a.Note.Summary)
			if r := strings.TrimSpace(a.Note.Rationale); r != "" {
				fmt.Fprintln(w, "      "+strings.ReplaceAll(r, "\n", "\n      "))
			}
		case a.Link != "":
			fmt.Fprintf(w, "  %s → %s\n", a.Label, a.Link)
		default:
			fmt.Fprintf(w, "  %s → %s\n", a.Label, a.Path)
		}
	}
}

// showSessionOverview prints the session overview res names, read from its
// worktree's session docs — wherever the caller runs.
func showSessionOverview(ctx context.Context, svc *domain.Service, res domain.Resolved, asJSON bool, stdout, stderr io.Writer) int {
	tsvc := openLinkTarget(res)
	cd, err := tsvc.GitCommonDir(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "overview show:", err)
		return 1
	}
	d, ok := openDocsAt("overview show", res.Checkout, config.SessionSteerDir(cd, res.Checkout), tsvc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	id := res.Hint.ID
	o, found := checkedOverview(d, id)
	if !found {
		fmt.Fprintf(stderr, "overview show: overview %s no longer exists\n", id)
		return 1
	}
	link, _ := domain.SessionOverviewLink(res.Checkout, id)
	repo := model.LinkRepo{Abs: filepath.ToSlash(res.Checkout)}
	anchors := anchorRows(o.Anchors, repo, "", func(n string) (agentdocs.Note, bool) { return d.store.FindNoteIn(d.root, n) })
	created := ""
	if !o.Created.IsZero() {
		created = o.Created.UTC().Format(time.RFC3339)
	}
	if asJSON {
		data, _ := json.MarshalIndent(map[string]any{"id": o.ID, "kind": "session", "link": link, "worktree": res.Checkout,
			"title": o.Title, "author": o.Author, "created": created, "text": o.Text, "anchors": anchors}, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	who := ""
	for _, s := range []string{o.Author, created} {
		if s != "" {
			who += ", " + s
		}
	}
	fmt.Fprintf(stdout, "%s — %s (session overview%s)\n%s\n\n%s\n", o.ID, o.Title, who, link, strings.TrimRight(o.Text, "\n"))
	printAnchors(stdout, anchors)
	return 0
}

// showReviewOverview prints review id's overview — its summary document's
// overview, anchors at the reviewed tip — or refuses a review without one.
func showReviewOverview(ctx context.Context, tsvc *domain.Service, id string, asJSON bool, stdout, stderr io.Writer) int {
	r, err := tsvc.Review(ctx, id)
	if err != nil {
		fmt.Fprintln(stderr, "overview show:", err)
		return 1
	}
	if !r.HasOverview() {
		fmt.Fprintf(stderr, "overview show: review %s has no overview — read it with gg review show <link>\n", r.ID)
		return 1
	}
	link, _ := tsvc.ReviewLink(ctx, r.ID)
	repo, commit, what := model.LinkRepo{}, "", "working changes"
	linked := true
	if r.Kind == domain.ReviewOnWorktree {
		repo = model.LinkRepo{Abs: filepath.ToSlash(r.Worktree)}
	} else {
		repo, linked = anchorRepo(ctx, tsvc, stderr)
		_, tip, _ := tsvc.ReviewRevs(ctx, r)
		if full, ok, err := tsvc.ResolveRev(ctx, tip); err == nil && ok {
			commit = strings.TrimSpace(full)
		} else {
			// The reviewed commit is gone: no commit to anchor at, and a
			// working-tree link would name whichever checkout answers.
			linked = false
		}
		what = short7(r.Commit)
	}
	if r.Branch != "" {
		what = r.Branch + " " + what
	}
	_, as := agentdocs.ParseOverview(r.Doc.Overview)
	anchors := anchorRows(as, repo, commit, nil)
	if !linked {
		anchors = unlinked(anchors)
	}
	if asJSON {
		data, _ := json.MarshalIndent(map[string]any{"id": r.ID, "kind": "review", "link": link, "agent": r.Agent,
			"commit": commit, "branch": r.Branch, "text": r.Doc.Overview, "anchors": anchors}, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintf(stdout, "%s — review by %s (%s)\n%s\n\n%s\n", r.ID, r.Agent, what, link, strings.TrimRight(r.Doc.Overview, "\n"))
	printAnchors(stdout, anchors)
	return 0
}
