package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// Skip reasons: the codes engine.SendSkip.Reason carries. English protocol
// (the CLI prints them); a frontend maps each to its own words.
const (
	SkipNotInPR       = "not in this PR"
	SkipLinesChanged  = "its lines changed"
	SkipBeingSent     = "already being sent"
	SkipOnGitHub      = "already on GitHub"
	SkipGone          = "it no longer exists"
	SkipThreadNotInPR = "its thread is not in this PR"
)

// SendSkipReasons lists every code (a frontend's translation gate).
func SendSkipReasons() []string {
	return []string{SkipNotInPR, SkipLinesChanged, SkipBeingSent, SkipOnGitHub, SkipGone, SkipThreadNotInPR}
}

// SendGroup is one group a "Send review" can pick (spec §3.5): "my draft
// review" or one AI review, with how many of its notes are still local.
type SendGroup struct {
	ID             string // GroupMine or "review:<id>"
	Agent, Summary string // a review's agent and summary line ("" for mine)
	Count          int
}

// PRSendGroups are PR n's local groups with something to send: "my draft
// review" first, then its AI reviews newest first. The PR's diff must be
// fetched (PRNotes says so otherwise).
func (s *Service) PRSendGroups(ctx context.Context, n int) ([]SendGroup, error) {
	byPath, err := s.PRNotes(ctx, n)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, rs := range byPath {
		for _, r := range rs {
			if r.Note.Source == model.NoteSourceForge || r.Note.IsForgeReply() || r.Sync == model.SyncSending || r.Sync == model.SyncForge {
				continue
			}
			counts[r.Group]++
		}
	}
	var out []SendGroup
	if c := counts[GroupMine]; c > 0 {
		out = append(out, SendGroup{ID: GroupMine, Count: c})
	}
	pr, err := s.PullRequest(ctx, n)
	if err != nil {
		return nil, err
	}
	prev, err := s.PRPreview(ctx, pr)
	if err != nil {
		return nil, err
	}
	for _, h := range s.prReviewHeads(ctx, prev.Set) {
		if c := counts["review:"+h.ID]; c > 0 {
			out = append(out, SendGroup{ID: "review:" + h.ID, Agent: h.Agent, Summary: h.Summary, Count: c})
		}
	}
	return out, nil
}

// ReviewBodyText is an AI review's summary as the user edits it before a
// send (the GitHub review body, unsigned and unmarked).
func (s *Service) ReviewBodyText(ctx context.Context, id string) (string, error) {
	r, err := s.Review(ctx, id)
	if err != nil {
		return "", err
	}
	if r.Doc != nil {
		return strings.TrimSpace(r.Doc.Summary), nil
	}
	return strings.TrimSpace(r.Text), nil
}
