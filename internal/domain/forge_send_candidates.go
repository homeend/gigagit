package domain

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// GroupReplies is the send panel's third group: the draft replies to
// GitHub threads (spec §5.1).
const GroupReplies = "replies"

// SendCandidates is everything the send panel lists for PR n (spec §5.1):
// every unsent local comment, grouped by source, each with what the
// planner would do with it today.
type SendCandidates struct {
	PR     int
	Head   string
	Groups []SendCandidateGroup
}

// SendCandidateGroup is one source: an AI review, the user's own notes, or
// the draft replies.
type SendCandidateGroup struct {
	ID      string // "review:<id>" | GroupMine | GroupReplies
	Kind    string // "review" | "mine" | "replies"
	Agent   string
	Title   string
	Created time.Time
	Slot    int
	Rows    []SendCandidate
}

// SendCandidate is one row: a note, a remark or a draft reply.
type SendCandidate struct {
	ID        string
	Kind      string // "note" | "remark" | "reply"
	Severity  string
	Path      string
	Range     [2]int
	Side      string
	Summary   string
	Rationale string
	Sync      model.SyncState
	Code      []string
	Skip      string
}

// PRSendCandidates lists PR n's send candidates: each AI review newest
// first, then "my notes", then the draft replies; rows by path then line.
// The PR's diff must be fetched (PRNotes says so otherwise). A row the
// planner would skip is listed with its reason (never hidden), so the
// panel can say why it cannot be ticked.
func (s *Service) PRSendCandidates(ctx context.Context, n int) (SendCandidates, error) {
	byPath, err := s.PRNotes(ctx, n)
	if err != nil {
		return SendCandidates{}, err
	}
	pr, err := s.PullRequest(ctx, n)
	if err != nil {
		return SendCandidates{}, err
	}
	prev, err := s.PRPreview(ctx, pr)
	if err != nil {
		return SendCandidates{}, err
	}
	out := SendCandidates{PR: n, Head: prev.Set.Tip}
	pl, err := s.sendPlacer(ctx, prev.Set, prev.Files)
	if err != nil {
		return SendCandidates{}, err
	}
	groups := map[string]*SendCandidateGroup{}
	group := func(id, kind string) *SendCandidateGroup {
		if g, ok := groups[id]; ok {
			return g
		}
		g := &SendCandidateGroup{ID: id, Kind: kind, Slot: GroupSlot(id)}
		if kind == "replies" {
			g.Slot = 0
		}
		groups[id] = g
		return g
	}
	reviews := map[string]Review{}
	for _, p := range PreviewNotePaths(byPath) {
		for _, r := range byPath[p] {
			// Draft replies hang under forge threads.
			for _, rep := range r.Replies {
				if rep.Note.IsForgeReply() && rep.Sync != model.SyncSending && rep.Sync != model.SyncForge {
					g := group(GroupReplies, "replies")
					g.Rows = append(g.Rows, s.candidateRow(ctx, rep, "reply", pl))
				}
			}
			if r.Note.Source == model.NoteSourceForge || r.Note.IsForgeReply() || r.Sync == model.SyncSending || r.Sync == model.SyncForge {
				continue
			}
			kind := "note"
			if model.IsReviewNoteID(r.Note.ID) {
				kind = "remark"
			}
			gkind := "mine"
			if r.Group != GroupMine {
				gkind = "review"
			}
			g := group(r.Group, gkind)
			if gkind == "review" {
				rid := strings.TrimPrefix(r.Group, "review:")
				if _, seen := reviews[rid]; !seen { // one read per review, on its first row
					rv, _ := s.Review(ctx, rid) // unreadable: the group stays, untitled, undated
					reviews[rid] = rv
					g.Agent, g.Created, g.Title = rv.Agent, rv.Created, candidateGroupTitle(rv)
				}
			}
			g.Rows = append(g.Rows, s.candidateRow(ctx, r, kind, pl))
		}
	}
	// Order: reviews newest first, mine, replies; rows by path then line.
	var revs []*SendCandidateGroup
	for _, g := range groups {
		if g.Kind == "review" {
			revs = append(revs, g)
		}
	}
	sortCandidateGroups(revs)
	for _, g := range revs {
		out.Groups = append(out.Groups, *g)
	}
	if g, ok := groups[GroupMine]; ok {
		out.Groups = append(out.Groups, *g)
	}
	if g, ok := groups[GroupReplies]; ok {
		out.Groups = append(out.Groups, *g)
	}
	for i := range out.Groups {
		rows := out.Groups[i].Rows
		sort.SliceStable(rows, func(a, b int) bool {
			if rows[a].Path != rows[b].Path {
				return rows[a].Path < rows[b].Path
			}
			return rows[a].Range[0] < rows[b].Range[0]
		})
	}
	return out, nil
}

// candidateGroupTitle is a review group's title: the first line of the
// review's summary — of its text when the review is prose (no document),
// the same source its send body starts from.
func candidateGroupTitle(r Review) string {
	text := r.Text
	if r.Doc != nil {
		text = r.Doc.Summary
	}
	title, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(title)
}

// sortCandidateGroups orders review groups newest first; two saved in the
// same instant go by id, so the panel's order never flips between reads.
func sortCandidateGroups(gs []*SendCandidateGroup) {
	sort.SliceStable(gs, func(i, j int) bool {
		if !gs[i].Created.Equal(gs[j].Created) {
			return gs[i].Created.After(gs[j].Created)
		}
		return gs[i].ID < gs[j].ID
	})
}

// candidateRow builds one row: its skip reason is what the planner would
// say today (noteItem / remarkItem against a scratch plan), its code the
// lines at the tip (the base for an old-side note), at most four.
func (s *Service) candidateRow(ctx context.Context, r ResolvedNote, kind string, pl *sendPlace) SendCandidate {
	n := r.Note
	row := SendCandidate{ID: n.ID, Kind: kind, Path: n.Address.Path, Range: r.Range, Side: string(n.Side),
		Summary: n.Summary, Rationale: n.Rationale, Sync: r.Sync, Severity: severityOf(n.Tags)}
	if kind != "reply" {
		var scratch engine.SendPlan
		if kind == "remark" {
			rid, i, _ := model.ParseReviewNoteID(n.ID)
			if rv, err := s.Review(ctx, rid); err == nil {
				s.remarkItem(ctx, &scratch, rv, i, pl)
			}
		} else {
			noteItem(&scratch, r, pl)
		}
		if len(scratch.Skipped) > 0 {
			row.Skip = scratch.Skipped[0].Reason
		}
	}
	if r.Range[0] > 0 {
		lines := pl.lines(n.Side == model.NoteSideOld, n.Address.Path)
		lo, hi := r.Range[0]-1, min(r.Range[1], r.Range[0]+3)
		if lo >= 0 && hi <= len(lines) && lo < hi {
			row.Code = append([]string(nil), lines[lo:hi]...)
		}
	}
	return row
}

// severityOf reads the "severity: <v>" tag a remark's meta folds into.
func severityOf(tags []string) string {
	for _, t := range tags {
		if v, ok := strings.CutPrefix(t, "severity: "); ok {
			return v
		}
	}
	return ""
}
