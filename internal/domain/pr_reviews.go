package domain

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// prReviews are the reviews that belong to a PR's view (plan 3, T1): one of
// the PR's commits is the reviewed tip, or the review was saved on the PR's
// preview link (its scope ends in "...refs/gg/pr/<n>").
func (s *Service) prReviews(ctx context.Context, set PreviewNoteSet) []Review {
	if _, ok := git.ParsePRRef(set.Source); !ok || !set.OK() {
		return nil
	}
	var out []Review
	for _, h := range s.prReviewHeads(ctx, set) {
		if r, err := s.Review(ctx, h.ID); err == nil && r.Doc != nil {
			out = append(out, r)
		}
	}
	return out
}

// prReviewHeads are the PR's review heads, newest first: commit and branch
// reviews (NoteCounts.Reviews) whose reviewed tip is one of the PR's commits,
// then the reviews saved on a preview whose source is the PR's ref
// (NoteCounts.PreviewReviews — a preview review is never in Reviews).
func (s *Service) prReviewHeads(ctx context.Context, set PreviewNoteSet) []ReviewHead {
	c, err := s.NoteCounts(ctx)
	if err != nil {
		return nil
	}
	in := set.commitSet()
	var out []ReviewHead
	for _, h := range c.Reviews {
		if in[h.Commit] {
			out = append(out, h)
		}
	}
	scopes := make([]string, 0, len(c.PreviewReviews))
	for sc := range c.PreviewReviews {
		if strings.HasSuffix(sc, "..."+set.Source) {
			scopes = append(scopes, sc)
		}
	}
	sort.Strings(scopes) // no map order
	for _, sc := range scopes {
		out = append(out, c.PreviewReviews[sc]...)
	}
	return out
}

// prReviewNotes is every unsent remark of the PR's reviews, by path, placed
// on the PR (new side: its head; old side: the merge base). A moved remark
// lives on GitHub; one whose lines changed stays in the review view only.
// Cached per tip:base:notes generation: READ-ONLY.
func (s *Service) prReviewNotes(ctx context.Context, set PreviewNoteSet) map[string][]ResolvedNote {
	if _, ok := git.ParsePRRef(set.Source); !ok || !set.OK() {
		return nil
	}
	s.mu.Lock()
	key := set.Tip + ":" + set.Base + ":" + strconv.FormatUint(s.notesGen, 10)
	if c, ok := s.prReviewCache[key]; ok {
		s.mu.Unlock()
		return c
	}
	gen := s.notesGen
	s.mu.Unlock()
	lines := map[string][]string{} // "<rev>:<path>" → lines, read once
	read := func(rev, path string) []string {
		k := rev + ":" + path
		if l, ok := lines[k]; ok {
			return l
		}
		var l []string
		if b, err := s.shaFile(ctx, rev, path); err == nil {
			l = splitLines(b)
		}
		lines[k] = l
		return l
	}
	out := map[string][]ResolvedNote{}
	for _, r := range s.prReviews(ctx, set) {
		threads, _ := r.RemarkThreads()
		i := -1
		for _, f := range r.Doc.Files {
			for _, dn := range f.Notes {
				i++
				path, side := reviewPath(f.Path), reviewSide(dn.Side)
				fp := remarkFP(f.Path, side, dn.Range, dn.Summary)
				if r.remarkMoved(fp) {
					continue
				}
				target := read(set.Tip, path)
				if side == model.NoteSideOld {
					target = read(set.Base, path)
				}
				rng, _, ok := s.remarkPlace(ctx, r, path, side, dn.Range, target)
				if !ok {
					continue
				}
				n := model.Note{ID: fmt.Sprintf("%s%s:%d", model.ReviewNoteIDPrefix, r.ID, i), Source: model.NoteSourceAgent,
					Author: r.Agent, Address: model.FileAddress{State: model.StateCommitted, Commit: set.Tip, Path: path},
					Side: side, Range: rng, Summary: dn.Summary, Rationale: dn.Rationale, Created: r.Created, Updated: r.Updated}
				for _, kv := range dn.Meta {
					n.Tags = append(n.Tags, kv.Key+": "+kv.Value)
				}
				sd := r.remarkSend(fp)
				rn := ResolvedNote{Note: n, Status: model.NoteActive, Range: rng, Group: "review:" + r.ID, Sync: sd.State()}
				if sd != nil {
					rn.SendErr = sd.Err
				}
				if i < len(threads) {
					rn.Resolution = threads[i].Resolution
					for _, rep := range threads[i].Replies {
						rep.Address, rep.Side, rep.Range = n.Address, side, rng
						rs, re := syncOf(rep)
						rn.Replies = append(rn.Replies, ResolvedNote{Note: rep, Status: model.NoteActive, Range: rng,
							Group: rn.Group, Sync: rs, SendErr: re})
					}
				}
				out[path] = append(out[path], rn)
			}
		}
	}
	s.mu.Lock()
	if s.notesGen == gen {
		if s.prReviewCache == nil {
			s.prReviewCache = map[string]map[string][]ResolvedNote{}
		}
		s.prReviewCache[key] = out
	}
	s.mu.Unlock()
	return out
}
