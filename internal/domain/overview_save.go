package domain

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notes"
	"github.com/homeend/gigagit/internal/textdiff"
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
	// The reads go through the gated domain queries (the per-repo
	// reservation), in batches that keep the command line short.
	inTree, inDir := map[string]bool{}, map[string]bool{}
	for _, batch := range batchPaths(clean) {
		found, err := s.TreePaths(ctx, t.Commit, batch)
		if err != nil {
			// Never "not in" for a check that did not run: say what failed.
			return []string{fmt.Sprintf("could not read %s's tree: %v", sha7, err)}
		}
		for _, p := range found {
			inTree[p] = true
		}
	}
	for _, p := range clean {
		if !inTree[p] {
			inDir[p] = treeHasDir(inTree, p)
		}
	}
	type fileLines struct {
		n                int
		binary, tooLarge bool
	}
	lines := map[string]fileLines{}
	count := func(path string) fileLines {
		if fl, ok := lines[path]; ok {
			return fl
		}
		fl := fileLines{n: -1}
		if data, err := s.ShowFile(ctx, t.Commit, path); err == nil {
			switch ls, ok := splitTextLines(data); {
			case ok:
				fl.n = len(ls)
			case len(data) > MaxDiffBytes && !textdiff.IsBinary(data):
				fl.tooLarge = true
			default:
				fl.binary = true
			}
		}
		lines[path] = fl
		return fl
	}
	var out []string
	said := map[string]bool{} // each problem line once
	say := func(line string) {
		if !said[line] {
			said[line] = true
			out = append(out, line)
		}
	}
	for _, sp := range spots {
		switch {
		case sp.note:
			say(sp.dest + ": a note link — link the lines instead")
		case inDir[sp.path]:
			say(sp.path + ": a directory — link a file")
		case !inTree[sp.path] || !cleanRepoPath(sp.path):
			say(shownPath(sp.path) + ": not in " + sha7)
		case sp.start == 0:
			// the whole file: present is enough
		default:
			fl := count(sp.path)
			switch {
			case fl.tooLarge:
				say(sp.path + ": too large to show its lines — link the file")
			case fl.binary:
				say(sp.path + ": binary — link the file, not its lines")
			case fl.n < 0:
				say(sp.path + ": not in " + sha7)
			case sp.end > fl.n:
				say(fmt.Sprintf("%s: past the end (%d lines at %s)", spotDest(sp.path, sp.start, sp.end), fl.n, sha7))
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
	// A disk check that cannot run refuses the save: permanence is enforced,
	// never assumed (R6).
	diskErr := func(err error) []string {
		return append(out, fmt.Sprintf("could not check the files on disk: %v", err))
	}
	dirty := map[string]bool{}
	for _, batch := range batchPaths(onDisk) {
		got, err := s.statusPaths(ctx, batch)
		if err != nil {
			return diskErr(err)
		}
		for _, p := range got {
			dirty[p] = true
		}
	}
	differs := map[string]bool{}
	head, ok, err := s.ResolveRev(ctx, "HEAD")
	if err != nil {
		return diskErr(err)
	}
	if ok && strings.TrimSpace(head) != t.Commit {
		for _, batch := range batchPaths(onDisk) {
			got, err := s.diffNamesBetween(ctx, t.Commit, "HEAD", batch)
			if err != nil {
				return diskErr(err)
			}
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

// treeHasDir: p is no file of the tree but a directory of it — ls-tree -r
// answers a directory pathspec with the files under it.
func treeHasDir(inTree map[string]bool, p string) bool {
	for f := range inTree {
		if strings.HasPrefix(f, p+"/") {
			return true
		}
	}
	return false
}

// statusPaths / diffNamesBetween are the disk check's reads under the
// per-repo reservation.
func (s *Service) statusPaths(ctx context.Context, paths []string) ([]string, error) {
	return query(ctx, s, "status-paths:"+strings.Join(paths, "\x00"), func(ctx context.Context) ([]string, error) {
		return s.repo.StatusPaths(ctx, paths)
	})
}

func (s *Service) diffNamesBetween(ctx context.Context, a, b string, paths []string) ([]string, error) {
	return query(ctx, s, "diff-names:"+a+".."+b+":"+strings.Join(paths, "\x00"), func(ctx context.Context) ([]string, error) {
		return s.repo.DiffNamesBetween(ctx, a, b, paths)
	})
}

// shownPath quotes a path holding a control character (a NUL from JSON),
// so the refusal prints it legibly.
func shownPath(p string) string {
	if strings.ContainsFunc(p, unicode.IsControl) {
		return strconv.Quote(p)
	}
	return p
}

// cleanRepoPath: a repo-relative slash path with no "." / ".." segment, no
// leading slash and no NUL (git cannot be asked about one; a tab or a
// newline is a legal file name) — the only form an anchor may name.
func cleanRepoPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.TrimSpace(p) != p || strings.ContainsRune(p, 0) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
