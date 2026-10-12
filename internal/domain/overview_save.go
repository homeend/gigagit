package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notes"
)

// Permanent overviews (spec 2026-10-12): an agent's overview stored on a
// commit, or on a pull request, as a commit-level note tagged "overview".
// This file is the ONE writer; overview_read.go the read model.

// ErrOverviewTarget refuses a link that names no single commit or PR.
var ErrOverviewTarget = errors.New("a permanent overview needs a commit or a pull request")

// OverviewTarget is where an overview is stored.
type OverviewTarget struct {
	Commit string // full sha the overview explains
	Branch string // recorded branch ("" = shows on the commit only)
	Scope  string // a PR overview's Note.Preview; "" for a commit
	PR     int    // the PR number; 0 for a commit
}

// OverviewRefused is a save gg refused: every problem, in document order
// (overview anchors first, then remarks, then the disk check).
type OverviewRefused struct{ Problems []string }

func (e *OverviewRefused) Error() string {
	return "the overview cannot be stored permanently:\n  " + strings.Join(e.Problems, "\n  ")
}

// SaveOverview is the one command that stores an overview.
type SaveOverview struct {
	Target OverviewTarget
	Agent  string
	Title  string // used when the document has no title
	Text   []byte // the document (fenced / enveloped is fine)
	DryRun bool   // check only; store nothing
}

// OverviewSaved is a stored (or, for a dry run, checked) overview.
type OverviewSaved struct {
	ID       string // "" for a dry run
	Replaced bool   // an overview with the same commit (or PR) and title was rewritten
	Doc      notebatch.OverviewDoc
}

// HeadOverviewTarget is HEAD, with the checked-out branch when HEAD is its
// tip (none when detached).
func (s *Service) HeadOverviewTarget(ctx context.Context) (OverviewTarget, error) {
	sha, ok, err := s.ResolveRev(ctx, "HEAD")
	if err != nil {
		return OverviewTarget{}, err
	}
	if !ok {
		return OverviewTarget{}, fmt.Errorf("%w: HEAD has no commit yet", ErrOverviewTarget)
	}
	sha = strings.TrimSpace(sha)
	return OverviewTarget{Commit: sha, Branch: s.reviewBranchName(ctx, "HEAD", sha)}, nil
}

// LinkOverviewTarget is the target a resolved gg:// link names: a commit; a
// branch tip (@ref:<name>, the branch recorded while it points there); or a
// pull request's link (<base>...refs/gg/pr/<n>). Anything else is
// ErrOverviewTarget.
func (s *Service) LinkOverviewTarget(ctx context.Context, res Resolved) (OverviewTarget, error) {
	switch {
	case res.Preview != nil:
		n, ok := git.ParsePRRef(res.Preview.Source)
		if !ok || !res.Preview.OK() {
			return OverviewTarget{}, ErrOverviewTarget
		}
		return OverviewTarget{Commit: res.Preview.Tip, Scope: res.Preview.scope(), PR: n}, nil
	case res.Pair != nil:
		return OverviewTarget{}, ErrOverviewTarget
	case res.Ref != "" && res.Commit != "":
		return OverviewTarget{Commit: res.Commit, Branch: s.reviewBranchName(ctx, res.Ref, res.Commit)}, nil
	case res.Addr.State == model.StateCommitted && res.Commit != "":
		return OverviewTarget{Commit: res.Commit}, nil
	}
	return OverviewTarget{}, ErrOverviewTarget
}

// SaveOverview checks the document against the target (spec §5) and stores
// it — or refuses with *OverviewRefused and stores nothing.
func (s *Service) SaveOverview(ctx context.Context, cmd SaveOverview) (OverviewSaved, string, error) {
	if !isFullSHA(cmd.Target.Commit) {
		return OverviewSaved{}, "", ErrOverviewTarget
	}
	doc, err := notebatch.ParseOverviewTitled(cmd.Text, cmd.Title)
	if err != nil {
		return OverviewSaved{}, "", err
	}
	if p := s.checkOverview(ctx, cmd.Target, doc); len(p) > 0 {
		return OverviewSaved{}, "", &OverviewRefused{Problems: p}
	}
	if cmd.DryRun {
		return OverviewSaved{Doc: doc}, "", nil
	}
	st := s.notesStore(ctx)
	if st == nil {
		return OverviewSaved{}, "", ErrNotesDisabled
	}
	replaced := false
	id, warn, err := s.saveNoteWithRetry(ctx, st, func() (string, error) {
		var perr error
		var id string
		id, replaced, perr = putOverview(st, cmd.Target, strings.TrimSpace(cmd.Agent), doc)
		return id, perr
	})
	if err != nil {
		return OverviewSaved{}, "", err
	}
	return OverviewSaved{ID: id, Replaced: replaced, Doc: doc}, warn, nil
}

// putOverview is one save attempt: the overview with the same commit (or
// the same PR) and title is rewritten in place, keeping its id and Created.
func putOverview(st notes.Store, t OverviewTarget, agent string, doc notebatch.OverviewDoc) (string, bool, error) {
	all, err := st.LoadAll()
	if err != nil {
		return "", false, err
	}
	now := notes.Now().UTC()
	n := model.Note{Source: model.NoteSourceAgent, Author: agent,
		Address: model.FileAddress{State: model.StateCommitted, Commit: t.Commit, Branch: t.Branch},
		Side:    model.NoteSideNew, Tags: []string{model.OverviewTag}, Preview: t.Scope,
		Summary: doc.Title, Rationale: string(doc.Canonical()), Created: now, Updated: now}
	replaced := false
	for _, old := range all {
		if old.IsReply() || !old.IsOverviewNote() || strings.TrimSpace(old.Summary) != doc.Title {
			continue
		}
		same := old.Preview == "" && t.Scope == "" && old.Address.Commit == t.Commit
		if t.Scope != "" {
			same = SameNoteScope(old.Preview, t.Scope)
		}
		if same {
			n.ID, n.Created, replaced = old.ID, old.Created, true
			break
		}
	}
	if n.ID == "" {
		n.ID = notes.NewID(all)
	}
	return n.ID, replaced, st.Put(n)
}

// overviewSpot is one place the document points at: an anchor or a remark.
type overviewSpot struct {
	dest       string // as reported: the anchor's dest, or "<path>:<a>[-<b>]"
	path       string
	start, end int // 0 = the whole file
	note       bool
}

func spotDest(path string, a, b int) string {
	if b > a {
		return fmt.Sprintf("%s:%d-%d", path, a, b)
	}
	return fmt.Sprintf("%s:%d", path, a)
}

// checkOverview is spec §5: every problem, in document order. One ls-tree
// for all paths, one read per file, one status (and for an older commit one
// diff) for the disk.
func (s *Service) checkOverview(ctx context.Context, t OverviewTarget, doc notebatch.OverviewDoc) []string {
	var spots []overviewSpot
	_, anchors := agentdocs.ParseOverview(doc.Overview)
	for _, a := range anchors {
		switch {
		case a.Note != "":
			spots = append(spots, overviewSpot{dest: a.Dest, note: true})
		case a.Path != "":
			spots = append(spots, overviewSpot{dest: a.Dest, path: a.Path, start: a.Start, end: max(a.End, a.Start)})
		}
	}
	for _, f := range doc.Files {
		for _, n := range f.Notes {
			spots = append(spots, overviewSpot{dest: spotDest(f.Path, n.Range[0], n.Range[1]), path: f.Path, start: n.Range[0], end: n.Range[1]})
		}
	}
	var paths []string
	seen := map[string]bool{}
	for _, sp := range spots {
		if sp.path != "" && !seen[sp.path] {
			seen[sp.path] = true
			paths = append(paths, sp.path)
		}
	}
	sha7 := t.Commit[:7]
	var clean []string // ls-tree fails on a path outside the repo: ask only about clean ones
	for _, p := range paths {
		if cleanRepoPath(p) {
			clean = append(clean, p)
		}
	}
	inTree := map[string]bool{}
	if found, err := s.repo.TreePaths(ctx, t.Commit, clean); err == nil {
		for _, p := range found {
			inTree[p] = true
		}
	}
	type fileLines struct {
		n      int
		binary bool
	}
	lines := map[string]fileLines{}
	count := func(path string) fileLines {
		if fl, ok := lines[path]; ok {
			return fl
		}
		fl := fileLines{n: -1}
		if data, err := s.ShowFile(ctx, t.Commit, path); err == nil {
			if ls, ok := splitTextLines(data); ok {
				fl.n = len(ls)
			} else {
				fl.binary = true
			}
		}
		lines[path] = fl
		return fl
	}
	var out []string
	reported := map[string]bool{} // "not in" once per path
	for _, sp := range spots {
		switch {
		case sp.note:
			out = append(out, sp.dest+": a note link — link the lines instead")
		case !inTree[sp.path] || !cleanRepoPath(sp.path):
			if !reported[sp.path] {
				reported[sp.path] = true
				out = append(out, sp.path+": not in "+sha7)
			}
		case sp.start == 0:
			// the whole file: present is enough
		default:
			fl := count(sp.path)
			switch {
			case fl.binary:
				out = append(out, sp.path+": binary — link the file, not its lines")
			case fl.n < 0:
				out = append(out, sp.path+": not in "+sha7)
			case sp.end > fl.n:
				out = append(out, fmt.Sprintf("%s: past the end (%d lines at %s)", spotDest(sp.path, sp.start, sp.end), fl.n, sha7))
			}
		}
	}
	if t.PR != 0 {
		return out // a PR is not the checkout: no disk check (spec §5a)
	}
	var onDisk []string
	for _, p := range paths {
		if inTree[p] && cleanRepoPath(p) {
			onDisk = append(onDisk, p)
		}
	}
	dirty := map[string]bool{}
	if got, err := s.repo.StatusPaths(ctx, onDisk); err == nil {
		for _, p := range got {
			dirty[p] = true
		}
	}
	differs := map[string]bool{}
	if head, ok, err := s.ResolveRev(ctx, "HEAD"); err == nil && ok && strings.TrimSpace(head) != t.Commit {
		if got, err := s.repo.DiffNamesBetween(ctx, t.Commit, "HEAD", onDisk); err == nil {
			for _, p := range got {
				differs[p] = true
			}
		}
	}
	for _, p := range onDisk {
		switch {
		case dirty[p]:
			out = append(out, p+": uncommitted changes")
		case differs[p]:
			out = append(out, p+": differs from "+sha7+" on disk")
		}
	}
	return out
}

// cleanRepoPath: a repo-relative slash path with no "." / ".." segment and
// no leading slash — the only form an anchor may name.
func cleanRepoPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.TrimSpace(p) != p {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
