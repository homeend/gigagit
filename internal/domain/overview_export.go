package domain

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/overviewhtml"
	"github.com/homeend/gigagit/internal/repogate"
	"github.com/homeend/gigagit/internal/syntax"
)

// NoteSpan is where a temporary note sits: what a note:t<n> anchor links to.
type NoteSpan struct {
	Path       string
	Start, End int
}

// OverviewSource names the overview to export: a stored review's
// (ReviewID) or an agent's in-memory one (Title + Text; NoteSpans resolve
// its note anchors — the note store lives in the live TUI or gg web). For
// an agent overview the service must be the overview's worktree.
type OverviewSource struct {
	ReviewID   string
	OverviewID string // a permanent overview (gg overview)
	Title      string
	Text       string
	NoteSpans  map[string]NoteSpan
}

// ErrNoAgentOverview: no overview with that id in this worktree.
var ErrNoAgentOverview = errors.New("no such overview")

// AgentOverviewSource is overview id's export source from the in-memory
// store a TUI or gg web holds, its note anchors resolved there; root is the
// caller's CheckoutKey (an overview of another worktree is not found).
func AgentOverviewSource(docs *agentdocs.Store, id, root string) (OverviewSource, error) {
	o, ok := docs.OverviewIn(root, id)
	if !ok {
		return OverviewSource{}, ErrNoAgentOverview
	}
	src := OverviewSource{Title: o.Title, Text: o.Text, NoteSpans: map[string]NoteSpan{}}
	for _, a := range o.Anchors {
		if a.Note == "" {
			continue
		}
		if n, found := docs.FindNoteIn(root, a.Note); found {
			src.NoteSpans[a.Note] = NoteSpan{Path: n.Path, Start: n.Start, End: n.End}
		}
	}
	return src, nil
}

// unparsedOverviewError is ErrNoOverview for a permanent overview whose
// stored document no longer parses, worded as that (ErrNoOverview's own text
// speaks of a review).
type unparsedOverviewError string

func (e unparsedOverviewError) Error() string {
	return "the stored document of overview " + string(e) + " no longer parses"
}

func (e unparsedOverviewError) Is(target error) bool { return target == ErrNoOverview }

// exportReader reads one anchored path's frozen bytes and its diff.
type exportReader struct {
	read func(path string) ([]byte, error)
	diff func(path string) (ReviewFileDiff, error)
}

// ExportOverviewHTML builds the self-contained page for src (spec
// 2026-10-12-overview-html-export): the overview, every anchored file at
// its frozen version (a stored review's tip, else the working tree) with
// its diff (base → tip, else HEAD → working tree), and no notes. name is
// <slug>.html. ErrNoOverview / ErrReviewNotFound pass through; a page over
// the cap is an *overviewhtml.TooLargeError.
func (s *Service) ExportOverviewHTML(ctx context.Context, src OverviewSource) ([]byte, string, error) {
	var (
		e       overviewhtml.Export
		doc     markdown.Doc
		anchors []agentdocs.Anchor
		ok      []bool // per anchor: may it open a file at all
		rd      exportReader
	)
	if src.OverviewID != "" {
		o, err := s.Overview(ctx, src.OverviewID)
		if err != nil {
			return nil, "", err
		}
		ov, err := s.overviewDocOf(ctx, o)
		if errors.Is(err, ErrNoOverview) {
			return nil, "", unparsedOverviewError(src.OverviewID)
		}
		if err != nil {
			return nil, "", err
		}
		doc = ov.Doc
		for _, a := range ov.Anchors {
			anchors, ok = append(anchors, a.Anchor), append(ok, a.OK)
		}
		e.Kind, e.Title = "review", "Overview: "+o.Title
		commit, r := o.Commit, s.overviewReview(ctx, o) // one read of the overview, not one per file
		rd = exportReader{
			read: func(p string) ([]byte, error) { return s.ShowFile(ctx, commit, p) },
			diff: func(p string) (ReviewFileDiff, error) { return s.reviewFileDiffOf(ctx, r, p) },
		}
	} else if src.ReviewID != "" {
		ov, err := s.ReviewOverview(ctx, src.ReviewID)
		if err != nil {
			return nil, "", err
		}
		r, err := s.Review(ctx, src.ReviewID)
		if err != nil {
			return nil, "", err
		}
		doc = ov.Doc
		for _, a := range ov.Anchors {
			anchors, ok = append(anchors, a.Anchor), append(ok, a.OK)
		}
		e.Kind, e.Title = "review", "Review overview: "+reviewTitleLabel(r)
		rd = exportReader{
			read: func(p string) ([]byte, error) { return s.overviewFileBytes(ctx, r, p) },
			diff: func(p string) (ReviewFileDiff, error) { return s.ReviewFileDiff(ctx, src.ReviewID, p) },
		}
	} else {
		doc, anchors = agentdocs.ParseOverview(src.Text)
		for i, a := range anchors {
			if a.Note != "" {
				sp, found := src.NoteSpans[a.Note]
				if found {
					anchors[i].Path, anchors[i].Start, anchors[i].End = sp.Path, sp.Start, sp.End
				}
				ok = append(ok, found)
				continue
			}
			ok = append(ok, a.Path != "")
		}
		e.Kind, e.Title = "agent", src.Title
		rd = exportReader{
			read: func(p string) ([]byte, error) { return s.WorktreeFile(ctx, p) },
			diff: func(p string) (ReviewFileDiff, error) { return s.WorkingFileDiff(ctx, p) },
		}
	}
	e.Blocks = doc.Blocks
	files, lineCount, err := s.exportFiles(ctx, rd, anchors, ok, &e)
	if err != nil {
		return nil, "", err
	}
	for i, a := range anchors {
		f := files[a.Path]
		missing := !ok[i] || f == nil
		if !missing && a.Start > 0 && f.Placeholder == "" {
			missing = max(a.Start, a.End) > lineCount[a.Path]
		}
		e.Anchors = append(e.Anchors, overviewhtml.Anchor{Dest: a.Dest, Path: a.Path, Start: a.Start, End: a.End, Label: a.Label, Missing: missing})
	}
	e.Repo, _ = s.RepoName(ctx)
	e.ExportedAt = clock.Now().Format("2006-01-02 15:04")
	e.Version = buildinfo.String()
	page, err := overviewhtml.Render(e)
	if err != nil {
		return nil, "", err
	}
	return page, overviewhtml.Slug(e.Title) + ".html", nil
}

// exportFiles reads each anchored path once, in anchor order, into e.Files
// and returns them by path with their line counts. A gone file is left out
// (its anchors draw missing), another read error is a placeholder; a
// cancelled or refused read fails the export, and so does passing the page
// cap — checked after every file, so a giant export stops early.
func (s *Service) exportFiles(ctx context.Context, rd exportReader, anchors []agentdocs.Anchor, ok []bool, e *overviewhtml.Export) (map[string]*overviewhtml.File, map[string]int, error) {
	files := map[string]*overviewhtml.File{}
	lineCount := map[string]int{}
	weight := 0
	for i, a := range anchors {
		if !ok[i] || a.Path == "" {
			continue
		}
		if _, seen := files[a.Path]; seen {
			continue
		}
		f, n, err := s.exportFile(rd, a.Path, e)
		var busy *repogate.BusyError
		switch {
		case err == nil:
		case ctx.Err() != nil || errors.As(err, &busy):
			return nil, nil, err // a cancelled or refused read: no hollow page
		case errors.Is(err, fs.ErrNotExist):
			continue // gone: its anchors are marked missing below
		default:
			f = &overviewhtml.File{Path: a.Path, Placeholder: fmt.Sprintf("could not read %s: %v", a.Path, err)}
		}
		files[a.Path], lineCount[a.Path] = f, n
		e.Files = append(e.Files, *f)
		if weight += overviewhtml.Weight(*f); weight > overviewhtml.MaxBytes {
			return nil, nil, overviewhtml.Oversized(e.Files)
		}
	}
	return files, lineCount, nil
}

// exportFile reads one anchored path: its lines (with syntax runs when
// highlighting is on) and its diff; a binary or too-large file is a
// placeholder. n is the line count (0 for a placeholder). The first diff's
// side labels become the page's range.
func (s *Service) exportFile(rd exportReader, path string, e *overviewhtml.Export) (*overviewhtml.File, int, error) {
	data, err := rd.read(path)
	if err != nil {
		return nil, 0, err
	}
	f := &overviewhtml.File{Path: path}
	if d, derr := rd.diff(path); derr == nil {
		f.Status, f.OldPath, f.Changed = d.Status, d.OldPath, d.Changed
		if e.Left == "" {
			e.Left, e.Right = d.Left, d.Right
		}
		if d.Changed && !d.Diff.Binary && !d.Diff.TooLarge {
			f.Diff = overviewhtml.Rows(d.Diff.Result.Rows, d.Diff.OldTok, d.Diff.NewTok)
		}
	}
	switch {
	case len(data) > MaxDiffBytes:
		f.Placeholder = fmt.Sprintf("%s is too large to include (%d bytes)", path, len(data))
		return f, 0, nil
	case IsBinary(data):
		f.Placeholder = fmt.Sprintf("%s is a binary file (%d bytes)", path, len(data))
		return f, 0, nil
	}
	lines, _ := splitTextLines(data)
	var tok [][]syntax.Tok
	if s.SyntaxHighlighting() && len(data) <= MaxSyntaxBytes && !HasBareCR(data) {
		if lang := syntax.Detect(path); lang != "" {
			tok = syntax.Lex(lang, data)
		}
	}
	f.Lines = overviewhtml.Lines(lines, tok)
	return f, len(lines), nil
}

// reviewTitleLabel names a review the way the TUI's reviewLabel does.
func reviewTitleLabel(r Review) string {
	switch {
	case r.Kind == ReviewOnWorktree:
		return "working changes"
	case r.Preview != "":
		return NoteScopeLabel(r.Preview)
	case r.Branch != "":
		return r.Branch + " " + reviewShort(r.Commit)
	}
	return reviewShort(r.Commit)
}
