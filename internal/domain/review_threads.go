package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notes"
)

// Review remarks are threads (spec 2026-10-05 review answers): a reply to
// remark "review:<id>:<n>" is a stored note at the REVIEW's address whose
// ParentID is the review's note id (an older gg keeps it as an ordinary
// reply) and whose Remark names the remark; a resolution is a [[resolved]]
// entry keyed by the remark id, re-keyed when a re-save moves the remark. Both find their remark again by
// fingerprint, so a re-saved review never moves an answer onto another
// remark.

// ErrNoSuchRemark is a remark index the review does not have.
var ErrNoSuchRemark = errors.New("no such remark")

// remarkFP fingerprints one remark: what it says and where.
func remarkFP(path string, side model.NoteSide, rng [2]int, summary string) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%d\x00%d\x00%s", reviewPath(path), side, rng[0], rng[1], summary))
	return hex.EncodeToString(h[:8])
}

// docRemark is one document remark in document order (index = n).
type docRemark struct {
	summary string
	fp      string
}

func (r Review) docRemarks() []docRemark {
	if r.Doc == nil {
		return nil
	}
	var out []docRemark
	for _, f := range r.Doc.Files {
		for _, dn := range f.Notes {
			side := reviewSide(dn.Side)
			out = append(out, docRemark{summary: dn.Summary, fp: remarkFP(f.Path, side, dn.Range, dn.Summary)})
		}
	}
	return out
}

// remarkFPs is every remark's fingerprint, by index.
func (r Review) remarkFPs() []string {
	var out []string
	for _, d := range r.docRemarks() {
		out = append(out, d.fp)
	}
	return out
}

// RemarkThread is what hangs off one remark: its replies (creation order)
// and its resolution (nil = open).
type RemarkThread struct {
	Remark     int
	Replies    []model.Note
	Resolution *model.ThreadResolution
}

// OutdatedThread is a thread whose remark the review no longer has: Root is
// the remark id it was made under, Summary the remark's summary as its first
// reply recorded it.
type OutdatedThread struct {
	Root, Summary string
	Replies       []model.Note
	Resolution    *model.ThreadResolution
}

// remarkFor finds the remark a thread made under root with fingerprint fp
// belongs to now: the same index when it still carries fp, else the remark
// that does, else -1 (outdated).
func remarkFor(rs []docRemark, root, fp string) int {
	if _, n, ok := model.ParseReviewNoteID(root); ok && n < len(rs) && rs[n].fp == fp {
		return n
	}
	for i, d := range rs {
		if d.fp == fp {
			return i
		}
	}
	return -1
}

// RemarkThreads joins r.Replies and r.Resolutions onto r's remarks: one
// entry per document remark (index = n), plus the threads whose remark the
// re-saved review no longer has.
func (r Review) RemarkThreads() (byRemark []RemarkThread, outdated []OutdatedThread) {
	rs := r.docRemarks()
	byRemark = make([]RemarkThread, len(rs))
	for i := range byRemark {
		byRemark[i].Remark = i
	}
	gone := map[string]*OutdatedThread{} // by fingerprint
	var order []string
	for _, n := range r.Replies {
		if i := remarkFor(rs, n.Remark, n.RemarkFP); i >= 0 {
			byRemark[i].Replies = append(byRemark[i].Replies, n)
			continue
		}
		o, ok := gone[n.RemarkFP]
		if !ok {
			o = &OutdatedThread{Root: n.Remark, Summary: n.RemarkSummary}
			gone[n.RemarkFP] = o
			order = append(order, n.RemarkFP)
		}
		o.Replies = append(o.Replies, n)
	}
	for i := range r.Resolutions {
		res := r.Resolutions[i]
		if j := remarkFor(rs, res.Root, res.RemarkFP); j >= 0 {
			byRemark[j].Resolution = &res
			continue
		}
		if o, ok := gone[res.RemarkFP]; ok {
			o.Resolution = &res
		} // no remark and no reply left: nothing to show
	}
	for _, fp := range order {
		outdated = append(outdated, *gone[fp])
	}
	return byRemark, outdated
}

// Tally is the review's remark count and how many of them are resolved; a
// remark moved to a forge is no longer the review's.
func (r Review) Tally() (remarks, resolved int) {
	th, _ := r.RemarkThreads()
	fps := r.remarkFPs()
	for i, t := range th {
		if i < len(fps) && r.remarkMoved(fps[i]) {
			continue
		}
		remarks++
		if t.Resolution != nil {
			resolved++
		}
	}
	return remarks, resolved
}

// remarkMoved reports a remark the forge now owns (hidden locally).
func (r Review) remarkMoved(fp string) bool {
	for _, x := range r.RemarkSends {
		if x.RemarkFP == fp && x.Moved {
			return true
		}
	}
	return false
}

// summaryFP keys the RemarkSends entry that records a review's SUMMARY
// reached a pull request (its whole-review send was submitted while some of
// its remarks stayed local, R4). Moved, so nothing counts it as sending; a
// remark fingerprint is hex, so it never collides. The entry is
// summaryKey(): this prefix plus the summary text's hash, so a review re-saved
// with a NEW summary has that summary unsent. A bare "summary" entry (written
// before the hash) still counts as sent: never post a summary twice.
const summaryFP = "summary"

// summaryKey is the summary mark of r's CURRENT summary text.
func (r Review) summaryKey() string {
	text := r.Text
	if r.Doc != nil {
		text = r.Doc.Overview
	}
	h := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return summaryFP + ":" + hex.EncodeToString(h[:8])
}

// summarySent reports the review's summary is already on pull request pr.
func (r Review) summarySent(pr int) bool {
	key := r.summaryKey()
	for _, x := range r.RemarkSends {
		if (x.RemarkFP == key || x.RemarkFP == summaryFP) && x.Moved && x.Send.PR == pr {
			return true
		}
	}
	return false
}

// remarkSend is the send entry of an unmoved remark fp (its sync state).
func (r Review) remarkSend(fp string) *model.NoteSend {
	for _, x := range r.RemarkSends {
		if x.RemarkFP == fp && !x.Moved {
			s := x.Send
			return &s
		}
	}
	return nil
}

// docTally counts a review document's remarks and how many of rs resolve
// one of them (by fingerprint, as RemarkThreads joins). No git: NoteCounts
// calls it for every review.
func docTally(text string, rs []model.ThreadResolution, sends []model.RemarkSend) (remarks, resolved int) {
	r := Review{Text: text, Resolutions: rs, RemarkSends: sends}
	if doc, err := notebatch.ParseReview([]byte(text)); err == nil {
		r.Doc = &doc
	}
	return r.Tally()
}

// remarkThreadSet is every remark reply and remark resolution of some parts,
// by review id: one read, shared by every review built from it.
type remarkThreadSet struct {
	replies     map[string][]model.Note
	resolutions map[string][]model.ThreadResolution
}

func loadReviewThreads(st notes.Store, all []model.Note, parts []notes.Part) remarkThreadSet {
	th := remarkThreadSet{replies: map[string][]model.Note{}, resolutions: map[string][]model.ThreadResolution{}}
	for _, n := range all {
		if n.IsRemarkReply() {
			id := n.ParentID
			th.replies[id] = append(th.replies[id], n)
		}
	}
	for id := range th.replies {
		rs := th.replies[id]
		sort.SliceStable(rs, func(a, b int) bool { return rs[a].Created.Before(rs[b].Created) })
	}
	for _, p := range parts {
		rs, err := st.LoadResolved(p)
		if err != nil {
			continue // an unreadable table hides resolutions, never fails a read
		}
		for _, res := range rs {
			if model.IsReviewNoteID(res.Root) {
				id := model.StoredRootID(res.Root)
				th.resolutions[id] = append(th.resolutions[id], res)
			}
		}
	}
	return th
}

// attach gives review r its remark replies and resolutions.
func (th remarkThreadSet) attach(r Review) Review {
	r.Replies, r.Resolutions = th.replies[r.ID], th.resolutions[r.ID]
	return r
}

// replyToRemark stores a reply to remark id ("review:<rid>:<n>") at the
// review's own address, stamped with the remark's fingerprint and summary.
func (s *Service) replyToRemark(ctx context.Context, id string, n model.Note) (model.Note, error) {
	return s.storeRemarkReply(ctx, id, nil, n)
}

// replyInThread answers an answer: the new reply joins the thread the
// answered one is in, wherever a re-save moved its remark (or outdated it).
func (s *Service) replyInThread(ctx context.Context, answered model.Note, n model.Note) (model.Note, error) {
	return s.storeRemarkReply(ctx, answered.Remark, &answered, n)
}

// storeRemarkReply is the one writer of remark replies. same, when set, is a
// reply of the thread: its remark stamps are copied as they are.
func (s *Service) storeRemarkReply(ctx context.Context, id string, same *model.Note, n model.Note) (model.Note, error) {
	rid, idx, ok := model.ParseReviewNoteID(id)
	if !ok {
		return model.Note{}, ErrNoteNotFound
	}
	st := s.notesStore(ctx)
	if st == nil {
		return model.Note{}, ErrNotesDisabled
	}
	all, err := st.LoadAll()
	if err != nil {
		return model.Note{}, err
	}
	var root *model.Note
	for i := range all {
		if all[i].ID == rid && !all[i].IsReply() && (all[i].IsReviewNote() || all[i].IsWorkingReview()) {
			root = &all[i]
			break
		}
	}
	if root == nil {
		return model.Note{}, fmt.Errorf("%w: %s", ErrReviewNotFound, rid)
	}
	if same != nil {
		n.Remark, n.RemarkFP, n.RemarkSummary = same.Remark, same.RemarkFP, same.RemarkSummary
	} else {
		rs := s.reviewOf(ctx, *root, map[string]string{}).docRemarks()
		if idx >= len(rs) {
			return model.Note{}, fmt.Errorf("%w: review %s has no remark %d", ErrNoSuchRemark, rid, idx)
		}
		n.Remark, n.RemarkFP, n.RemarkSummary = id, rs[idx].fp, rs[idx].summary
	}
	now := notes.Now().UTC()
	n.ID = notes.NewID(all)
	n.ParentID = rid // the review note: an ordinary stored parent
	if n.Source == "" {
		n.Source = model.NoteSourceUser
	}
	n.Address, n.Side, n.Range, n.ContextHash = root.Address, model.NoteSideNew, [2]int{}, ""
	n.Created, n.Updated = now, now
	if err := st.Put(n); err != nil {
		return model.Note{}, err
	}
	s.invalidateNoteCounts()
	return n, nil
}
