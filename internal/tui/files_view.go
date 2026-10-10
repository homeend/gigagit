package tui

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// filesMode is the files view's source mode — exactly one is active while the
// view is open. It is the authoritative discriminator; the inCompareMode/
// inFullTree helpers read it (they replaced the old filesCompare/filesAllFiles
// booleans). It is set only by the transition methods below.
type filesMode int

const (
	filesModeChanged  filesMode = iota // a commit's changed files (vs parent)
	filesModeFullTree                  // every file at the commit (ls-tree); `a` toggle
	filesModeCompare                   // two endpoints (filesLeft/filesRight)
	filesModeStash                     // a stash's files (filesStashTag)
	filesModeShelf                     // a shelved commit's frozen files (filesShelfID)
	filesModeWorktree                  // every file on disk, fuzzy-filtered (F; wtFiles)
)

func (m Model) inCompareMode() bool { return m.filesMode == filesModeCompare }
func (m Model) inFullTree() bool    { return m.filesMode == filesModeFullTree }
func (m Model) inShelfFiles() bool  { return m.filesMode == filesModeShelf }

// filesCommitBadges reports whether the files view lists ONE commit's files
// whose rows take its ByCommitPath ◆ badges. A review tree paints its own.
func (m Model) filesCommitBadges() bool {
	return m.filesHash != "" && m.filesReview == nil &&
		(m.filesMode == filesModeChanged || m.filesMode == filesModeFullTree)
}

// closeFilesView closes the view and zeroes the ENTIRE cluster — the single
// place that defines "no files view is open". Replaces the per-site partial
// resets (esc, l, narrow-close, repo-switch) that each cleared a different subset.
func (m Model) closeFilesView() Model {
	m.filesMode = filesModeChanged
	m.filesView = nil
	m.filesTitle = ""
	m.filesContext = ""
	m.filesCommit = model.Commit{}
	m.filesHash = ""
	m.filesLeft = model.Endpoint{}
	m.filesRight = model.Endpoint{}
	m.compareTag = ""
	m.comparePair = nil
	m.filesSets = nil
	// A link compare still loading was asked for by the view that just
	// closed (or by one a newer open supersedes): drop it on arrival.
	m.linkCompareWant = ""
	// inCompareMode() is true for a plain branch/pair compare too, so a preview
	// scope left behind here would stamp the NEXT compare as a preview and make
	// its rows note-addressable at a stale tip. This is the single exit point.
	m.filesPreviewSet = nil
	m.filesPreviewCounts = nil
	m.filesPreviewGroups = nil
	m.filesPreviewReviews = nil
	m.filesPairLabel = ""
	m.filesReview = nil
	m.filesLandNote = ""
	m.filesLandScope = ""
	m.filesBack = nil
	m.filesStashTag = ""
	m.filesShelfID = ""
	m.filesShelfLabel = ""
	m.filesShelfNotes = nil
	m.filesTreeFocused = false
	m.filesReadInflight = false
	m.filesPreview = nil
	m.previewOpen = nil
	m.wtFiles = nil
	m.filesFull = false
	m.previewFull = false
	// The popup that handed off to this view is dropped with it: only the
	// view's own esc/l close (which reads the field BEFORE calling this)
	// returns to it. A repo switch, a steer navigation, a narrow terminal or
	// a fresh open from the panels all tear the view down for a reason that
	// makes the parked popup meaningless. See handOffToFilesView.
	m.filesReturnLayers = nil
	m.treeSlept, m.treeKeepPath = false, ""
	// A merge preview resolve in flight was dispatched for the view that just
	// closed: bump the generation so its result is dropped instead of
	// re-opening the view behind the user (handlePreviewOpenMsg re-stamps the
	// generation when it opens, so a fresh open is unaffected).
	m.previewGen++
	return m
}

// beginFilesView is every opener's clean slate. A fresh open remembers the
// source panel for esc/l to restore. A re-open inside a live view (enter on
// the commit list to drill into the tree, a switcher opened over the view)
// keeps the panel AND the popup parked by handOffToFilesView: the window is
// still the one that popup opened, so closing it must still return there.
func (m Model) beginFilesView() Model {
	if m.filesView == nil {
		m.filesReturnFocus = m.focus
		return m.closeFilesView()
	}
	parked := m.filesReturnLayers
	m = m.closeFilesView()
	m.filesReturnLayers = parked
	return m
}

// openChangedFiles opens a commit's changed-file list (mode=Changed), setting the
// complete consistent set from a clean slate (clears any prior compare/stash/
// fullTree/preview state). Opens on the commit-list side (treeFocused=false).
func (m Model) openChangedFiles(c model.Commit) (Model, tea.Cmd) {
	m = m.beginFilesView()
	m.filesView = &contentPopup{lines: []contentLine{{text: i18n.T("(loading…)")}}}
	m.filesTitle = i18n.T("Files %s %s", shortHash(c.Hash), c.Subject)
	m.filesContext = shortHash(c.Hash) + " " + c.Subject
	m.filesCommit = c // paint the date immediately when the feed row already knows it; the load confirms
	m.filesHash = c.Hash
	m.filesMode = filesModeChanged
	m.filesReadInflight = true
	return m, m.loadCommitFilesCmd(c)
}

// filesMetaLine renders the files view's under-title line: the commit's AUTHOR
// date, then its author. "" when the date is unknown — the view then draws no
// line at all (and spends no row on it), which beats parking an "(unknown)"
// placeholder under every header the lookup could not resolve.
func filesMetaLine(c model.Commit) string {
	if c.UnixTime == 0 {
		return ""
	}
	s := commitDateString(c) // the same stamp the i popup shows, from one formatter
	if c.Author != "" {
		s += " · " + c.Author
	}
	return s
}

// filesMetaLineFor is the date line the view would draw right now: "" unless a
// single-commit mode is open with a known date. Only the two single-commit
// modes have one date behind them — a compare has two endpoints, and
// stash/shelf keep their own headers.
func (m Model) filesMetaLineFor() string {
	if m.filesReview != nil {
		return reviewMetaLine(m.filesReview)
	}
	if m.filesMode != filesModeChanged && m.filesMode != filesModeFullTree {
		return ""
	}
	return filesMetaLine(m.filesCommit)
}

// openStashFiles opens a stash's files (mode=Stash) from a clean slate. Opens on
// the stash-list side (treeFocused=false), like the commit files view.
func (m Model) openStashFiles(ref, subject string) (Model, tea.Cmd) {
	m = m.beginFilesView()
	m.filesView = &contentPopup{lines: []contentLine{{text: i18n.T("(loading…)")}}}
	m.filesTitle = i18n.T("Files %s %s", ref, subject)
	m.filesContext = ref + " " + subject
	m.filesStashTag = ref
	m.filesMode = filesModeStash
	return m, m.loadStashFilesCmd(ref)
}

// openShelfCommitFiles opens a shelved commit's frozen files (mode=Shelf) from
// a clean slate: the tar's members in the tree, each carrying the standard
// focused-file actions (diff vs working tree, Copy to working dir, …) through
// the shelf-member FileRef. Invoked from the G switcher through
// handOffToFilesView (the files view is not a layer and must not draw under
// the popup; the switcher is parked and comes back on esc/l), so this opener
// never touches the layer stack itself.
func (m Model) openShelfCommitFiles(e model.ShelfEntry) (Model, tea.Cmd) {
	m = m.beginFilesView()
	m.filesView = &contentPopup{lines: []contentLine{{text: i18n.T("(loading…)")}}}
	m.filesTitle = i18n.T("Files %s", shelfEntryDisplay(e))
	m.filesContext = shelfEntryDisplay(e)
	m.filesMode = filesModeShelf
	m.filesShelfID = e.ID
	m.filesShelfLabel = i18n.T("shelf #%s", shortShelf(e))
	// The tree owns the view: a shelved commit is not part of the feed, so
	// there is no live commit list to follow on the right (compare-mode rule).
	m.filesTreeFocused = true
	return m, m.loadShelfFilesCmd(e.ID)
}

// shelfFilesMsg carries a shelved commit's member list, tagged with the entry
// id so a stale result (view closed / another entry opened) is dropped.
type shelfFilesMsg struct {
	slotStamp // the slot it was asked from (slot_msg.go)
	id        string
	files     []model.CommitFile
	notes     []domain.ResolvedNote // the entry's own notes (a Notes section above the members)
	err       error
}

// loadShelfFilesCmd lists the shelved commit's tar members off the UI thread,
// with the entry's own notes. A notes read that fails only drops the section.
func (m Model) loadShelfFilesCmd(entryID string) tea.Cmd {
	svc := m.svc
	slot := m.stamp()
	return func() tea.Msg {
		ctx := context.Background()
		files, err := svc.ShelfCommitFiles(ctx, entryID)
		notes, _ := svc.ShelfNotes(ctx, entryID)
		return shelfFilesMsg{slotStamp: slot, id: entryID, files: files, notes: notes, err: err}
	}
}

// toggleFullTree flips between a commit's changed files and its full tree,
// dropping any open preview and reloading. Caller guards that a commit files
// view (not stash/compare) is open.
func (m Model) toggleFullTree() (Model, tea.Cmd) {
	if m.inFullTree() {
		m.filesMode = filesModeChanged
	} else {
		m.filesMode = filesModeFullTree
	}
	m.filesPreview = nil
	if p := m.filesView; p != nil {
		p.lines = []contentLine{{text: i18n.T("(loading…)")}}
		p.sel = 0
		p.query = ""
	}
	m.filesReadInflight = true
	return m, m.loadFilesForCmd(m.filesViewCommit())
}

// focusTree / focusRight move focus within an open files view. focusRight is
// inert in compare and shelf modes (no commit-list side to focus).
func (m Model) focusTree() Model { m.filesTreeFocused, m.previewFull = true, false; return m }
func (m Model) focusRight() Model {
	if !m.inCompareMode() && !m.inShelfFiles() {
		m.filesTreeFocused = false
	}
	return m
}

// closePreview drops the right-column file preview and returns focus to the tree.
func (m Model) closePreview() Model {
	if d := m.filesPreview; d != nil {
		m.openFiles.remove(m.currentWorktree, d) // closed on purpose: out of the open files
	}
	m.filesPreview = nil
	return m.focusTree()
}

// commitFileLines renders a commit's changed files as content lines:
// root-level files first (no heading), then one bold heading per directory
// (its full path) with the directory's files indented beneath. Exactly one
// heading level — no nesting. Sorting is dir-major because a plain path sort
// interleaves a directory's files with its subdirectories, which would emit
// the same heading twice.
func commitFileLines(files []model.CommitFile) []contentLine {
	if len(files) == 0 {
		return []contentLine{{text: i18n.T("(no files)")}}
	}
	sorted := make([]model.CommitFile, len(files))
	copy(sorted, files)
	sort.SliceStable(sorted, func(a, b int) bool {
		da, db := path.Dir(sorted[a].Path), path.Dir(sorted[b].Path)
		if da != db {
			return da < db // "." sorts before any directory name
		}
		return sorted[a].Path < sorted[b].Path
	})

	out := make([]contentLine, 0, len(sorted))
	lastDir := ""
	for _, f := range sorted {
		dir := path.Dir(f.Path)
		if dir == "." {
			out = append(out, contentLine{text: fileLine(f), path: f.Path, oldPath: f.OldPath, status: f.Status})
			continue
		}
		if dir != lastDir {
			out = append(out, contentLine{text: dir + "/", heading: true})
			lastDir = dir
		}
		out = append(out, contentLine{text: "  " + fileLine(f), path: f.Path, oldPath: f.OldPath, status: f.Status})
	}
	return out
}

// withReviewLines puts a commit's reviews in front of its file list as a
// "Reviews" heading. The stack is built from this list, so the
// reviews read above the first real file there too.
func withReviewLines(reviews []domain.Review, lines []contentLine) []contentLine {
	if len(reviews) == 0 {
		return lines
	}
	out := make([]contentLine, 0, len(reviews)+1+len(lines))
	out = append(out, contentLine{text: i18n.T("Reviews"), heading: true})
	for _, r := range reviews {
		// No path: a review is not a file, so every file action (copy, shelf,
		// bookmark, compare, the stack) passes it by; its menu is noteRowMenu.
		out = append(out, contentLine{text: "  " + reviewRowText(r), noteID: r.ID})
	}
	if len(lines) == 1 && lines[0].path == "" && !lines[0].heading {
		return out // "(no files)": the reviews are the whole list
	}
	return append(out, lines...)
}

// notedElsewhere lists, sorted, the paths with notes at commit hash that the
// commit does not change — counted in its Commits ◆ N, so its Files view must
// reach them too. plain counts only the notes written in NO scope: a range
// review's notes are its own row (withScopeLines), never loose paths here.
func notedElsewhere(plain map[string]int, hash string, files []model.CommitFile) []string {
	changed := make(map[string]bool, len(files))
	for _, f := range files {
		changed[f.Path] = true
	}
	var out []string
	for k, n := range plain {
		p, ok := strings.CutPrefix(k, hash+":")
		if ok && n > 0 && !changed[p] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// scopeLabel names a note scope the Previews panel's way round.
func scopeLabel(scope string) string { return domain.NoteScopeLabel(scope) }

// withScopeLines puts a commit's range reviews in front of its list under a
// "Range reviews" heading: one row per commit pair its notes were written in
// (the caller passes the ones the viewed branch shows — Model.shownScopes).
// A pair's notes all sit on its newer commit, mostly on files that commit
// does not change — so the row stands for the whole review (every note of
// the scope at this commit, whichever file), and enter opens the range where
// each note is on its file.
func withScopeLines(scopes []domain.NoteScopeCount, lines []contentLine) []contentLine {
	if len(scopes) == 0 {
		return lines
	}
	out := make([]contentLine, 0, len(scopes)+1+len(lines))
	out = append(out, contentLine{text: i18n.T("Range reviews"), heading: true})
	for _, sc := range scopes {
		out = append(out, contentLine{text: "  " + scopeLabel(sc.Scope), noteScope: sc.Scope})
	}
	if len(lines) == 1 && lines[0].path == "" && !lines[0].heading {
		return out // "(no files)": the reviews are the whole list
	}
	return append(out, lines...)
}

// scopeNoteCount is how many notes of scope the commit holds.
func scopeNoteCount(c domain.NoteCounts, hash, scope string) int {
	for _, sc := range c.ScopesByCommit[hash] {
		if sc.Scope == scope {
			return sc.N
		}
	}
	return 0
}

// withNotedLines puts those paths in front of a commit's files under a
// "Notes" heading (the way withReviewLines lists its reviews).
func withNotedLines(paths []string, lines []contentLine) []contentLine {
	if len(paths) == 0 {
		return lines
	}
	out := make([]contentLine, 0, len(paths)+1+len(lines))
	out = append(out, contentLine{text: i18n.T("Notes"), heading: true})
	for _, p := range paths {
		out = append(out, contentLine{text: "  " + p, notedPath: p})
	}
	if len(lines) == 1 && lines[0].path == "" && !lines[0].heading {
		return out // "(no files)": the notes are the whole list
	}
	return append(out, lines...)
}

// withShelfNoteLines puts a shelved set's own notes in front of its members as
// a "Notes" heading — the way withReviewLines lists a commit's reviews. A note
// row has no path: it is not a file, so every file action passes it by.
func withShelfNoteLines(notes []domain.ResolvedNote, lines []contentLine) []contentLine {
	if len(notes) == 0 {
		return lines
	}
	out := make([]contentLine, 0, len(notes)+1+len(lines))
	out = append(out, contentLine{text: i18n.T("Notes"), heading: true})
	for _, r := range notes {
		lead, summary := shelfNoteRowText(r.Note)
		out = append(out, contentLine{text: "  " + lead + summary, shelfNote: r.Note.ID, elideHead: len([]rune("  " + lead))})
	}
	if len(lines) == 1 && lines[0].path == "" && !lines[0].heading {
		return out // "(no files)": the notes are the whole list
	}
	return append(out, lines...)
}

// shelfNoteRowText is a note's row under the Notes heading, split into its
// lead and its summary: "└ 2026-09-29 14:02 " + "Recycled from /x/wt-a (feat)".
// A narrow list cuts only the summary (which names a path) in its middle.
func shelfNoteRowText(n model.Note) (lead, summary string) {
	lead = "└ "
	if !n.Created.IsZero() {
		lead += n.Created.Local().Format("2006-01-02 15:04") + " "
	}
	return lead, sanitizeLine(n.Summary)
}

// reviewRowText is a review's row under the Reviews heading:
// "└ 2026-09-28 18:12 Claude Code" (the heading already says it is a review).
func reviewRowText(r domain.Review) string {
	var parts []string
	if !r.Created.IsZero() {
		parts = append(parts, r.Created.Local().Format("2006-01-02 15:04"))
	}
	if a := strings.TrimSpace(r.Agent); a != "" {
		parts = append(parts, sanitizeLine(a))
	}
	if len(parts) == 0 {
		parts = append(parts, sanitizeLine(r.Summary))
	}
	remarks, resolved := r.Tally()
	return withTally("└ "+strings.Join(parts, " "), remarks, resolved, tallyNarrow)
}

// fileLine renders one file row: "<letter>  <basename>"; renames show the
// full old path and the new basename.
func fileLine(f model.CommitFile) string {
	if f.OldPath != "" {
		return f.Status + "  " + f.OldPath + " → " + path.Base(f.Path)
	}
	return f.Status + "  " + path.Base(f.Path)
}

// commitFilesMsg carries one commit's changed files, tagged with the hash so
// stale results from fast j/k movement can be dropped. commit is the resolved
// commit behind the list (date/author filled in for a bare sha).
type commitFilesMsg struct {
	slotStamp // the slot it was asked from (slot_msg.go)
	hash      string
	subject   string
	commit    model.Commit
	files     []model.CommitFile
	reviews   []domain.Review // the commit's AI reviews (@notes/)
	// noReviews marks a follow-live read of the list alone: the reviews
	// follow once the cursor rests (reviewsFollowMsg).
	noReviews bool
	err       error
}

// loadFilesListCmd is loadFilesForCmd for a follow-live move: the file list
// alone (a cache hit once prefetched — the step then shows at once), never
// the commit's reviews, which cost a notes-store read and are read once the
// cursor rests (reviewsFollowMsg).
func (m Model) loadFilesListCmd(c model.Commit) tea.Cmd {
	if m.inFullTree() {
		return m.loadTreeFilesCmd(c)
	}
	svc := m.svc
	slot := m.stamp()
	return func() tea.Msg {
		c = resolveCommitMeta(svc, c)
		files, err := svc.CommitFiles(context.Background(), c.Hash)
		return commitFilesMsg{slotStamp: slot, hash: c.Hash, subject: c.Subject, commit: c, files: files, err: err, noReviews: true}
	}
}

// filesPrefetchBelow / filesPrefetchAbove are how many commits around the
// cursor a landed file list warms (PrefetchCommitFiles), in list order —
// more below, the way a held arrow usually goes.
const (
	filesPrefetchBelow = 16
	filesPrefetchAbove = 4
)

// prefetchFilesCmd warms the file lists of the commits around the Commits
// cursor off the UI thread, so a held arrow's next steps are cache hits and
// show at once instead of waiting a git call each on a slow disk. nil when
// there is nothing to warm (a full tree is not cached).
func (m Model) prefetchFilesCmd() tea.Cmd {
	if m.svc == nil || m.inFullTree() {
		return nil
	}
	idx := m.displayIndices(panelCommits)
	s := m.sel[panelCommits]
	var hashes []string
	for r := s - filesPrefetchAbove; r <= s+filesPrefetchBelow; r++ {
		if r == s || r < 0 || r >= len(idx) || m.isWipRow(idx[r]) {
			continue
		}
		if bi := idx[r] - m.wipCount(); bi >= 0 && bi < len(m.commits) && !m.svc.CommitFilesCached(m.commits[bi].Hash) {
			hashes = append(hashes, m.commits[bi].Hash)
		}
	}
	if len(hashes) == 0 {
		return nil
	}
	svc := m.svc
	return func() tea.Msg {
		svc.PrefetchCommitFiles(context.Background(), hashes, 4)
		return nil
	}
}

// reviewsFollowDelay is how long the commit cursor must rest before the files
// view reads the commit's reviews.
const reviewsFollowDelay = 150 * time.Millisecond

// reviewsFollowMsg is the pause after a follow-live list landed: read the
// commit's reviews unless a later landing (a newer gen) superseded it.
type reviewsFollowMsg struct {
	slotStamp // the slot it was asked from (slot_msg.go)
	gen       int
	hash      string
}

// commitReviewsMsg carries a commit's reviews read after its file list.
type commitReviewsMsg struct {
	hash    string
	reviews []domain.Review
}

// reviewsFollowCmd schedules the reviews read for the list just shown.
func (m Model) reviewsFollowCmd(hash string) (Model, tea.Cmd) {
	m.reviewsFollowGen++
	gen, slot := m.reviewsFollowGen, m.stamp()
	return m, m.tick(reviewsFollowDelay, func(time.Time) tea.Msg { return reviewsFollowMsg{slotStamp: slot, gen: gen, hash: hash} })
}

// onReviewsFollow reads the rested cursor's reviews.
func (m Model) onReviewsFollow(msg reviewsFollowMsg) (Model, tea.Cmd) {
	if msg.gen != m.reviewsFollowGen || m.filesView == nil || msg.hash != m.filesHash || m.svc == nil {
		return m, nil
	}
	svc, hash := m.svc, msg.hash
	return m, func() tea.Msg {
		reviews, _ := svc.ReviewsForCommit(context.Background(), hash) // no store: no reviews
		return commitReviewsMsg{hash: hash, reviews: reviews}
	}
}

// onCommitReviews puts the reviews on top of the list already shown, keeping
// the tree cursor on the row it was on.
func (m Model) onCommitReviews(msg commitReviewsMsg) (Model, tea.Cmd) {
	p := m.filesView
	// A branch's review shows on that branch only (domain.ReviewsShownOn).
	msg.reviews = domain.ReviewsShownOn(msg.reviews, m.viewBranches())
	if p == nil || msg.hash != m.filesHash || m.filesReview != nil || m.filesMode != filesModeChanged || len(msg.reviews) == 0 {
		return m, nil
	}
	for _, l := range p.lines {
		if l.noteID != "" {
			return m, nil // already listed (the list was read with them)
		}
	}
	before := len(p.lines)
	p.lines = withReviewLines(msg.reviews, p.lines)
	if p.sel > 0 {
		p.sel += len(p.lines) - before
	}
	return m, nil
}

// loadCommitFilesCmd fetches the changed files of commit c, and its reviews,
// off the UI thread.
func (m Model) loadCommitFilesCmd(c model.Commit) tea.Cmd {
	svc := m.svc
	slot := m.stamp()
	return func() tea.Msg {
		c = resolveCommitMeta(svc, c)
		files, err := svc.CommitFiles(context.Background(), c.Hash)
		reviews, _ := svc.ReviewsForCommit(context.Background(), c.Hash) // no store: no reviews
		return commitFilesMsg{slotStamp: slot, hash: c.Hash, subject: c.Subject, commit: c, files: files, reviews: reviews, err: err}
	}
}

// resolveCommitMeta fills in a commit's date/author/subject when the caller
// only had a hash — Tags, goto-SHA and the reflog all synthesize a bare
// model.Commit. Runs on the load goroutine, so the extra read never touches the
// UI thread, and is best-effort: an unresolvable rev leaves c as it was and the
// header simply carries no date. Commits that came from the feed already have
// the fields and cost nothing here.
func resolveCommitMeta(svc *domain.Service, c model.Commit) model.Commit {
	if c.UnixTime != 0 || c.Hash == "" {
		return c
	}
	full, err := svc.CommitMeta(context.Background(), c.Hash)
	if err != nil {
		return c
	}
	c.UnixTime, c.Author = full.UnixTime, full.Author
	if c.Subject == "" {
		c.Subject = full.Subject // a goto-SHA open starts hash-only; the title improves too
	}
	return c
}

// treeFilesMsg carries the full file tree of commit hash, with the content lines
// already built (the dir-major sort runs off the UI thread — the tree can be
// 10^4–10^5 files on a large repo).
type treeFilesMsg struct {
	slotStamp // the slot it was asked from (slot_msg.go)
	hash      string
	subject   string
	commit    model.Commit
	lines     []contentLine
	err       error
}

// staleFor: the slot gave its full tree up while sleeping (sleepFWindow)
// and the kick re-reads it on return — the answer in flight would be
// dropped at replay, so the gate drops it now rather than queue the tree.
func (msg treeFilesMsg) staleFor(v *worktreeView) bool {
	return v.windows.treeSlept
}

// loadTreeFilesCmd fetches every file in commit c's tree (ls-tree) AND builds the
// content lines off the UI thread, so the render thread only assigns the result.
func (m Model) loadTreeFilesCmd(c model.Commit) tea.Cmd {
	svc := m.svc
	slot := m.stamp()
	return func() tea.Msg {
		c = resolveCommitMeta(svc, c)
		files, err := svc.TreeFiles(context.Background(), c.Hash)
		if err != nil {
			return treeFilesMsg{slotStamp: slot, hash: c.Hash, subject: c.Subject, commit: c, err: err}
		}
		return treeFilesMsg{slotStamp: slot, hash: c.Hash, subject: c.Subject, commit: c, lines: commitFileLines(files)}
	}
}

// loadFilesForCmd loads the files-view content for commit c in the active mode:
// the full tree (every file at the commit) in full-tree mode, else the changed
// set. Used wherever the view (re)loads so the mode sticks across navigation.
func (m Model) loadFilesForCmd(c model.Commit) tea.Cmd {
	if m.inFullTree() {
		return m.loadTreeFilesCmd(c)
	}
	return m.loadCommitFilesCmd(c)
}

// filesViewCommit returns the loaded commit the files view is showing (matched
// by filesHash). Outside the feed — a Tags / goto-SHA / reflog open — it falls
// back to the commit the load resolved, so the i popup shows the same date the
// header does instead of "(unknown)"; then to a hash-only Commit.
func (m Model) filesViewCommit() model.Commit {
	for i := range m.commits {
		if m.commits[i].Hash == m.filesHash {
			return m.commits[i]
		}
	}
	if m.filesCommit.Hash == m.filesHash && m.filesHash != "" {
		return m.filesCommit
	}
	return model.Commit{Hash: m.filesHash}
}

// filesSearchLine is the /-search line under the title ("" = none): the
// working-tree window keeps its fuzzy query apart from filesView.query.
func (m Model) filesSearchLine() string {
	if m.inWorktreeFiles() {
		return m.wtSearchLine()
	}
	return m.filesView.searchLine()
}

// filesChromeLines counts the lines above the rows inside the box: the title,
// the date line (commit modes) and the /-search line. The one source for the
// renderer and the reveal tooltip, so they can never disagree on where a row
// sits on screen.
func (m Model) filesChromeLines() int {
	n := 1
	if m.filesMetaLineFor() != "" {
		n++
	}
	if m.filesSearchLine() != "" {
		n++
	}
	return n
}

// filesRowsCap is how many lines the box gives the rows (sticky line
// included): its height minus the borders and the chrome, floored at one.
func (m Model) filesRowsCap(boxH int) int {
	n := boxH - 2 - m.filesChromeLines()
	if n < 1 {
		n = 1
	}
	return n
}

// filesGeom is the files view's row-window geometry: the slice of vis the
// frame builds (window-then-build keeps a 10^5-row tree O(visible)), the
// cursor within it, and — in the single-line modes, where every row is one
// line — the first row shown. The renderer and the reveal tooltip both read
// it, so the tooltip lands on the row the renderer painted.
type filesGeom struct {
	s0, s1, anchor int  // vis[s0:s1] is the window; anchor the cursor within it
	sticky         bool // a sticky heading line leads the rows (rowsCap ≥ 2)
	top            int  // the first row shown, an index into the window
	headTop        bool // the top row is a heading: it IS the sticky line
}

// filesGeometry lays vis out in rowsCap lines around sel. With a sticky line
// the rows get rowsCap-1 lines; when the first of them is a heading it moves
// up into the sticky line and the rows start beneath it (no heading twice).
func filesGeometry(vis []contentLine, sel, rowsCap int) filesGeom {
	g := filesGeom{s1: len(vis), anchor: sel, sticky: rowsCap >= 2}
	if len(vis) > 2*rowsCap+1 {
		if g.s0 = sel - rowsCap; g.s0 < 0 {
			g.s0 = 0
		}
		if g.s1 = sel + rowsCap + 1; g.s1 > len(vis) {
			g.s1 = len(vis)
		}
		g.anchor = sel - g.s0
	}
	body := rowsCap
	if g.sticky {
		body--
	}
	g.top = windowStart(g.s1-g.s0, body, g.anchor)
	g.headTop = g.sticky && g.s0+g.top < len(vis) && (vis[g.s0+g.top].heading || vis[g.s0+g.top].summary || vis[g.s0+g.top].overviewDoc)
	return g
}

// cursorLine is the cursor's line within the rows area (0 = the first line,
// the sticky one when there is one) in the single-line modes.
func (g filesGeom) cursorLine() int {
	if !g.sticky || g.headTop { // the heading moved up: its rows start on line 1
		return g.anchor - g.top
	}
	return 1 + g.anchor - g.top
}

// renderFileRows lays the tree's rows out in rowsCap lines, the first of which
// always names the directory of the row beneath it — a sticky heading, so a
// long directory scrolled past its own heading still says where its files
// are ("." for root files). When the window's first row IS a heading it takes
// that place itself and the rows start beneath it, so no heading shows twice.
// wr is the built window vis[g.s0:g.s1]; the heading scan runs over vis so a
// heading before the window is still found. In wrap mode the first row is
// only known after the layout, so it comes from renderWindowTop rather than g.
func (m Model) renderFileRows(wr []winRow, vis []contentLine, g filesGeom, innerW, rowsCap int) []string {
	p := m.filesView
	if !g.sticky {
		return renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: p.mode, anchor: g.anchor, hscroll: p.hscroll})
	}
	body, top := renderWindowTop(wr, winOpts{w: innerW, h: rowsCap - 1, mode: p.mode, anchor: g.anchor, hscroll: p.hscroll})
	sticky := wr[top]
	if vis[g.s0+top].heading || vis[g.s0+top].summary || vis[g.s0+top].overviewDoc {
		// The heading itself is the sticky line; its files fill the body.
		body, _ = renderWindowTop(wr[top+1:], winOpts{w: innerW, h: rowsCap - 1, mode: p.mode, anchor: g.anchor - top - 1, hscroll: p.hscroll})
	} else {
		text := "."
		for i := g.s0 + top - 1; i >= 0; i-- {
			if vis[i].heading {
				text = vis[i].text
				break
			}
		}
		sticky = winRow{text: text, style: st().titleStyle}
	}
	sticky.text = elidePath(sticky.text, innerW) // one line whatever the mode
	head := renderWindow([]winRow{sticky}, winOpts{w: innerW, h: 1, mode: modeCutoff})
	return append(head, body...)
}

// lineHash resolves the commit a tree line's content lives in: a per-line
// sha (a -u stash's untracked ^3 parent) wins over the view-wide hash.
func (m Model) lineHash(l contentLine) string {
	if l.sha != "" {
		return l.sha
	}
	return m.filesHash
}

// canShowFilesViewMessage reports whether i in the files view can show the
// displayed commit's message: a real single-commit view (not a stash or a
// compare, which have no single commit behind them), idle, with a resolved
// hash. The single source of truth shared by the key handler and the footer so
// they cannot advertise a dead binding.
func (m Model) canShowFilesViewMessage() bool {
	return m.filesView != nil && m.opsIdle() &&
		m.stashView == nil && !m.inCompareMode() && m.filesHash != ""
}

// compareFilesMsg carries a whole-tree comparison's changed files, tagged so a
// superseded load (fast re-open) can be dropped.
type compareFilesMsg struct {
	slotStamp // the slot it was asked from (slot_msg.go)
	tag       string
	files     []model.CommitFile
	err       error
}

// openCompareFiles opens the files view in compare mode for the endpoint pair
// (left = older, right = newer), e.g. a commit vs the working tree. The proven
// single-commit path is untouched; this is a parallel mode (filesModeCompare).
func (m Model) openCompareFiles(left, right model.Endpoint) (Model, tea.Cmd) {
	// compareTagFor is the FIRST thing that touches these values and it calls
	// Endpoint.CacheTag(), which PANICS by design on an unresolved ref (a
	// moving name must never become a cache key) and on the zero Endpoint. A
	// panic here takes the whole Bubble Tea process with it, so an endpoint
	// this view cannot identify is declined with a status line instead.
	//
	// Unreachable from today's two domain→TUI conduits, which hand back only
	// commit and shelf endpoints — but this is the branch that teaches domain
	// to construct new kinds, so the boundary is asserted rather than assumed.
	// Resolve a ref through domain.EvalEndpoint before opening a compare.
	if !endpointComparable(left) || !endpointComparable(right) {
		m.statusMsg = i18n.T("no commit selected to compare against")
		return m, nil
	}
	tag := compareTagFor(left, right)
	// Already showing (or loading) this exact comparison: keep it — re-running
	// the load would only blank and repaint identical content. Each caller
	// orders its endpoints deterministically, so the same pair from the same
	// gesture always builds the same tag.
	if m.filesView != nil && m.inCompareMode() && m.compareTag == tag {
		return m, nil
	}
	m = m.beginFilesView() // clean slate: clears any prior changed/stash/fullTree/preview state
	m.filesView = &contentPopup{lines: []contentLine{{text: i18n.T("(loading…)")}}}
	m.filesTitle = left.Display() + " ↔ " + right.Display()
	m.filesContext = m.filesTitle
	m.filesMode = filesModeCompare
	m.filesLeft = left
	m.filesRight = right
	m.filesHash = compareFilesHash(left, right)
	m.compareTag = tag
	// Focus the tree: compare mode has no live commit list, and moving the commit
	// selection would discard the comparison. The focus-switch keys are inert here.
	m.filesTreeFocused = true
	return m, m.loadCompareFilesCmd(left, right, tag)
}

// compareFilesHash is a compare view's h/b (history/blame) context: the commit
// side, preferring the right; "" means the working tree. Shared by the
// endpoint-shaped and the link-shaped opener so the two cannot drift.
//
// A PAIR counts, and its NEWER side is the commit — PairB() is a resolved sha
// (model.PairEndpoint requires 7..64 hex on both halves), and the pair's own
// byte source is b, so history/blame stand where the compare's content does.
// Without those arms a pair fell to the default and silently gave those
// surfaces the working tree.
func compareFilesHash(left, right model.Endpoint) string {
	switch {
	case right.Kind() == model.EndpointCommit:
		return right.Hash()
	case right.Kind() == model.EndpointPair:
		return right.PairB()
	case left.Kind() == model.EndpointCommit:
		return left.Hash()
	case left.Kind() == model.EndpointPair:
		return left.PairB()
	}
	return ""
}

// loadCompareFilesCmd fetches the changed-file list off the UI thread.
func (m Model) loadCompareFilesCmd(left, right model.Endpoint, tag string) tea.Cmd {
	svc := m.svc
	slot := m.stamp()
	return func() tea.Msg {
		files, err := svc.CompareFiles(context.Background(), left, right)
		return compareFilesMsg{slotStamp: slot, tag: tag, files: files, err: err}
	}
}

// shortHash truncates a sha to 7 characters for display.
func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// filesPageRows is the tree's visible row capacity: the left column's box
// height minus borders (2), the title line (1), the hint line (1), and — in the
// single-commit modes — the date line (1). It must track renderFilesView's
// rowsCap, or pgup/pgdn oversteps the window it draws.
func (m Model) filesPageRows() int {
	n := m.layout().bodyH - 3
	if m.filesMetaLineFor() != "" {
		n--
	}
	if n < 1 {
		n = 1
	}
	return n
}

// updateFilesViewKey routes keys while the files view is open. ←/→/tab pick
// which side owns vertical movement (filesTreeFocused); the commits side
// keeps the follow-live reload; ctrl+↑/↓ always scrolls the tree; /-search,
// close keys and quit are focus-independent; everything else is swallowed.
func (m Model) updateFilesViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.filesView
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// Order (spec §4.7): a search being TYPED owns every key, then the line
	// selection, then a committed query, then the preview's own esc.
	// previewSearchKey covers the first and third in one call, so the selection
	// hook runs first and declines while the search is typing.
	if nm, cmd, handled := m.previewSelectKey(msg); handled {
		return nm, cmd
	}
	// The focused preview's in-view search comes next: it owns / @ ] [ and,
	// while a query is live, esc — otherwise esc still closes the preview below.
	if nm, cmd, handled := m.previewSearchKey(msg); handled {
		return nm, cmd
	}
	// …then the focused preview's notes: } { step, d / r act on the note
	// under the cursor. It declines a key it has nothing to do with.
	if nm, cmd, handled := m.previewNoteKey(msg); handled {
		return nm, cmd
	}
	// …and an overview's anchors in the file it opened: n / p (declined
	// without bands, so the review view's n / p keep working).
	if nm, cmd, handled := m.bandKey(msg); handled {
		return nm, cmd
	}
	if m.inWorktreeFiles() {
		return m.updateWorktreeFilesKey(msg)
	}
	if p.typing { // /-input mode captures every key (same as the help window)
		// Arrows/pages move the tree selection live while typing (no cursor reset),
		// like the commit filter; j/k stay query text.
		if filterMotion(msg, p.move, m.filesPageRows()) {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEsc:
			p.typing = false
			p.query = ""
			p.sel = 0
		case tea.KeyEnter:
			p.typing = false // commit: search stays active
		case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete:
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
			}
			p.sel = 0
		case tea.KeySpace:
			p.query += " "
			p.sel = 0
		case tea.KeyRunes:
			p.query += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
	switch msg.String() {
	case "n", "p": // the review view: the next / previous file with notes
		if m.filesReview != nil && m.filesTreeFocused {
			m.stepReviewFile(msg.String() == "n")
			return m, nil
		}
	case ".":
		return m.openActionMenu(), nil
	case "g": // global bookmark quick-switcher
		return m.openBookmarkSwitcher()
	case "G": // global shelf quick-switcher
		return m.openShelfSwitcher()
	case "F": // the working-tree files window (F)
		return m.openWorktreeFilesHere()
	case "r": // a PR's file list: re-read its review comments (inert elsewhere)
		if m.openPRNumber() > 0 {
			return m.prCommentsCmd(true)
		}
	case "i": // show the displayed commit's message, mirroring the Commits-panel i
		if m.openPRNumber() > 0 { // a PR diff has no commit of its own: i is its hub
			return m.openPRHubFromDiff()
		}
		// Resolve the commit the tree is ACTUALLY showing (filesViewCommit, keyed
		// by filesHash) — not the Commits-panel cursor: a reflog/tags-opened view
		// sets focus=panelCommits but displays a different commit. The popup layers
		// OVER the tree (esc returns to it) — filesView is untouched.
		if m.canShowFilesViewMessage() {
			return m.openCommitMessagePopup(m.filesViewCommit())
		}
		return m, nil
	case "ctrl+w":
		if m.filesPreview != nil && !m.filesTreeFocused { // z cycles the focused preview
			m.filesPreview.p.mode = m.filesPreview.p.mode.next()
			m.filesPreview.p.hscroll = 0
			return m, nil
		}
		p.mode = p.mode.next()
		p.hscroll = 0
		return m, nil
	case "a": // toggle full-tree (every file at this commit) vs the changed set
		if m.stashView != nil || m.inCompareMode() || m.filesHash == "" || m.filesReview != nil {
			return m, nil // only meaningful for a commit files view (not its review mode)
		}
		return m.toggleFullTree()
	// The commit-list side IS the Commits panel selection (m.focus stays
	// panelCommits), so the graph window keys mirror the base panel exactly
	// (model.go). Only on the file-tree side do shift+arrows scroll the tree.
	case ">":
		if !m.filesTreeFocused && m.filesPreview == nil && m.graphActive() {
			m.commitGraphCols = m.clampCols(m.graphCols() + m.graphStep())
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
		}
		return m, nil
	case "<":
		if !m.filesTreeFocused && m.filesPreview == nil && m.graphActive() {
			m.commitGraphCols = m.clampCols(m.graphCols() - m.graphStep())
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
		}
		return m, nil
	case "=":
		if !m.filesTreeFocused && m.filesPreview == nil && m.graphActive() {
			m = m.snapGraphToSelected()
		}
		return m, nil
	case "shift+left":
		if !m.filesTreeFocused && m.filesPreview == nil && m.graphActive() {
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll - m.graphPanStep())
			return m, nil
		}
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if !m.filesTreeFocused && m.filesPreview == nil && m.graphActive() {
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll + m.graphPanStep())
			return m, nil
		}
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
		return m, nil
	// q is inert here: only the base layout quits on q. esc is the back key;
	// ctrl+c (handled above) remains the universal quit.
	case "ctrl+t": // a focused preview fills the screen, and back; the tree likewise
		if m.filesPreview != nil && !m.filesTreeFocused {
			m.previewFull = !m.previewFull
		} else if m.filesTreeFocused {
			m.filesFull = !m.filesFull // long paths show whole
		}
		return m, nil
	case "ctrl+]":
		if m.filesPreview != nil { // step the preview aside; the file stays open
			return m.backgroundDoc(m.filesPreview), nil
		}
		return m, nil
	case "X":
		if m.filesPreview != nil && !m.filesTreeFocused { // close the file for good
			return m.closePreview(), nil
		}
		return m, nil
	case "esc":
		if m.filesPreview != nil { // the preview is the topmost surface — close it first
			if m.filesPreview.backgrounded { // it has been in the background: back there
				return m.backgroundDoc(m.filesPreview), nil
			}
			m = m.closePreview() // returns focus to the tree (the source of View file)
			return m, nil
		}
		if p.query != "" { // first esc clears the committed search
			p.query = ""
			p.sel = 0
			return m, nil
		}
		return m.leaveReviewView()
	case "l":
		ret, parked := m.filesReturnFocus, m.filesReturnLayers
		m = m.closeFilesView()
		m.focus = ret
		return m.restoreParkedLayers(parked), nil
	case "/":
		// A FOCUSED preview already took / above (its in-view text search); the
		// tree side keeps its own filter even while a preview is open.
		// Focus decides the search target. The commit-list side routes to the
		// base commit filter (which the right column already renders); the tree
		// side, and the stash list (which has no base filter), filter the tree.
		if !m.filesTreeFocused && m.stashView == nil {
			m.filterPanel = panelCommits
			m.filterQuery = ""
			m.filterTyping = true
			// Cursor stays put (the main `/` entry rule): typing snaps to the
			// nearest match at/after it instead of restarting from the top.
			return m, nil
		}
		p.typing = true
		p.query = ""
		p.sel = 0
	case "h":
		if !m.filesTreeFocused {
			return m, nil
		}
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) || vis[p.sel].path == "" {
			return m, nil
		}
		ctx := navContext{path: vis[p.sel].path, rev: m.lineHash(vis[p.sel])}
		hv := newHistoryView(ctx)
		m = m.pushLayer(hv)
		return m, m.loadHistoryListCmd(hv)
	case "b":
		if !m.filesTreeFocused {
			return m, nil
		}
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) || vis[p.sel].path == "" {
			return m, nil
		}
		ctx := navContext{path: vis[p.sel].path, rev: m.lineHash(vis[p.sel])}
		bv := newBlameView(ctx)
		m = m.pushLayer(bv)
		return m, m.loadBlameCmd(ctx, bv.tag)
	case "f": // branch-pair compare: cycle the origin filter (all / left / right)
		if !m.inCompareMode() || m.comparePair == nil {
			return m, nil
		}
		return m.cycleCompareScope(), nil
	case "enter":
		if !m.filesTreeFocused {
			// List side: enter already drilled in, so on a stash it is inert
			// (the Apply/Pop/Drop actions live in the "." menu). For a commit
			// list, enter moves focus to the file tree, mirroring enter on the
			// Commits panel.
			if m.stashView != nil {
				return m, nil
			}
			return m.focusTree(), nil
		}
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) || (vis[p.sel].path == "" && !vis[p.sel].summary && !vis[p.sel].overviewDoc && vis[p.sel].noteID == "" && vis[p.sel].shelfNote == "" && vis[p.sel].notedPath == "" && vis[p.sel].noteScope == "") {
			return m, nil // heading row, placeholder, or empty view
		}
		return m.openDiffForFileLine(vis[p.sel])
	case "left":
		m = m.focusTree()
	case "right":
		m = m.focusRight() // inert in compare mode (no commit-list side)
	case "tab", "shift+tab":
		if !m.inCompareMode() {
			if m.filesTreeFocused {
				m = m.focusRight()
			} else {
				m = m.focusTree()
			}
		}
	case "alt+up", "alt+down":
		// The LINE CURSOR of a focused preview; ↑/↓ keep scrolling the viewport
		// only. Inert on the tree side and with no preview open, where alt+↑/↓
		// belong to the search-recall dropdown (handled above while typing).
		if m.filesPreview != nil && !m.filesTreeFocused {
			delta := 1
			if msg.String() == "alt+up" {
				delta = -1
			}
			m.movePreviewCursor(delta)
		}
		return m, nil
	case "up", "k":
		if m.filesTreeFocused {
			p.move(-1)
			return m, nil
		}
		return m.moveListUnderFilesView(-1)
	case "down", "j":
		if m.filesTreeFocused {
			p.move(1)
			return m, nil
		}
		return m.moveListUnderFilesView(1)
	case "ctrl+up": // always the tree, from either side
		p.move(-1)
	case "ctrl+down":
		p.move(1)
	case "pgup":
		if m.filesTreeFocused {
			p.move(-m.filesPageRows())
			return m, nil
		}
		return m.moveListUnderFilesView(-m.pageStep())
	case "pgdown":
		if m.filesTreeFocused {
			p.move(m.filesPageRows())
			return m, nil
		}
		return m.moveListUnderFilesView(m.pageStep())
	}
	return m, nil
}

// previewSearchKey gives the file preview's in-view search first refusal on a
// key. It runs BEFORE the tree's own /-filter typing branch, so a query typed
// into the preview can never land in the tree's filter, and only while the
// preview owns the right column (the tree side keeps its own / entirely).
func (m Model) previewSearchKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	p, rows, inner, ok := m.activePreview()
	if !ok {
		return m, nil, false
	}
	if p.search.typing {
		nm, cmd, ev := m.searchTypingKey(&p.search, msg)
		m = nm
		switch ev {
		case searchChanged, searchCommitted:
			p.search.refindFrom(previewSearchLines(p), p.search.origin)
			if p.search.cur >= 0 {
				p.snapHit(rows, inner)
			} else {
				p.sel, p.cur, p.hscroll = p.searchOrig.sel, p.searchOrig.cur, p.searchOrig.hscroll
			}
		case searchCancelled:
			p.sel, p.cur, p.hscroll = p.searchOrig.sel, p.searchOrig.cur, p.searchOrig.hscroll
		}
		return m, cmd, true
	}
	switch searchCommandKey(&p.search, msg) {
	case searchOpenFwd, searchOpenBack:
		p.searchOrig = previewOrigin{sel: p.sel, cur: p.cur, hscroll: p.hscroll}
		p.search.open(msg.String() == "@", p.searchPos(rows))
		return m.recallReset(), nil, true
	case searchNext:
		p.search.cur = stepHit(p.search.hits, p.searchPos(rows), 1)
		p.snapHit(rows, inner)
		return m, nil, true
	case searchPrev:
		p.search.cur = stepHit(p.search.hits, p.searchPos(rows), -1)
		p.snapHit(rows, inner)
		return m, nil, true
	case searchCleared:
		p.search.clear()
		return m, nil, true
	case searchIgnored:
		return m, nil, true
	}
	return m, nil, false
}

// openDiffForFileLine opens the full-screen diff for one files-view tree row,
// in the tree's active mode: full-tree (commit ↔ working tree), compare (the
// endpoint pair), or the plain commit (parent ↔ commit) diff. Shared by the
// files-view enter handler and Home/End file-stepping; records diffNav=tree so
// the stepper knows the source list. Refuses to open below 60 columns.
func (m Model) openDiffForFileLine(l contentLine) (tea.Model, tea.Cmd) {
	if m.width > 0 && m.width < 60 {
		m.statusMsg = i18n.T("terminal too narrow for the diff view")
		return m, nil
	}
	if l.summary {
		return m.openReviewSummary()
	}
	if l.overviewDoc {
		return m.openReviewOverviewDoc()
	}
	if l.shelfNote != "" { // a shelved set's note: the read-only note popup, esc comes back here
		for _, r := range m.filesShelfNotes {
			if r.Note.ID == l.shelfNote {
				return m.openShelfNotes(shelfNotesMsg{id: m.filesShelfID, label: m.filesContext, notes: []domain.ResolvedNote{r}}), nil
			}
		}
		return m, nil
	}
	if l.noteScope != "" { // a commit's Range review row: the range opens, esc comes back here
		return m.openScopeRow(l.noteScope)
	}
	if l.notedPath != "" { // a commit's Notes row: its notes read in the viewer, esc comes back here
		return m, m.commitNotesCmd(m.filesHash, l.notedPath)
	}
	if l.noteID != "" { // an @notes/ entry: the review opens as the review view, esc comes back here
		if bp := m.previewReturnHere(); bp != nil && m.inCompareMode() {
			// A preview's Reviews block: esc re-opens the preview.
			return m.openReviewWith(l.noteID, reviewTitle(bp.title), model.Commit{}, bp)
		}
		back := m.filesCommit
		if back.Hash == "" {
			back.Hash = m.filesHash
		}
		return m.openReviewFrom(l.noteID, reviewTitle(shortHash(m.filesHash)), back)
	}
	m.diffNotice = "" // drop any stale notice; the stepper re-posts its arrival notice
	m.diffNav = diffNavTree
	if m.diffStacked && !m.inFullTree() {
		// The stacked preference is on: every file of this list opens in one
		// scroll, positioned on the file that was picked (design §4).
		return m.openStack(diffNavTree, l.path, nil)
	}
	newV := &diffView{
		title:   l.path,
		context: "@ " + m.filesContext,
		rev:     m.filesHash,
		loading: true,
		partial: m.diffPartial,
		long:    m.diffLong,
	}
	if dv := m.diffLayer(); dv != nil {
		*dv = *newV // stepping: reuse the entry already on the stack
	} else {
		m = m.pushLayer(newV)
	}
	m.stampPreviewNotes(m.diffLayer(), l.path)
	m.stampReviewNotes(m.diffLayer(), l.path)
	cmd, tag, context := m.treeFileLoad(l)
	m.diffLayer().context = context
	m.diffTag = tag
	return m, cmd
}

// stampPreviewNotes gives a merge preview's file view its note address. A
// merge preview is a compare whose NEW side is the source tip, so its rows ARE
// note-addressable at that commit — unlike every other compare, whose old side
// no stored address names (a no-op there). Both openers call it: the
// single-file view before its loader runs (the loader inherits it from the
// layer), and the stack as each file lands — its loader inherits from the
// STACK view, which names no file.
func (m Model) stampPreviewNotes(dv *diffView, path string) {
	set := m.filesPreviewSet
	if dv == nil || !m.inCompareMode() || set == nil {
		return
	}
	dv.previewSet = set
	dv.noteAddr = model.FileAddress{State: model.StateCommitted, Commit: set.Tip, Path: path}
}

// treeFileLoad picks the loader for ONE files-view row: the Cmd (which yields
// a diffMsg), its tag, and the context line the view's header shows. It is
// openDiffForFileLine's choice of loader with no layer bookkeeping, so the
// stacked view can run the very same loaders per file (diff_stack.go) without
// a second copy of this dispatch.
func (m Model) treeFileLoad(l contentLine) (cmd tea.Cmd, tag, context string) {
	switch {
	case m.inFullTree():
		// Full-tree mode: the file may be unchanged in this commit, so a
		// parent-diff would be empty. Diff the commit's version against the
		// working tree instead — useful for any file in the tree.
		left := mustCommitEndpoint(m.filesHash)
		right := model.WorkTreeEndpoint()
		tag = "cmp:" + left.CacheTag() + ":" + right.CacheTag() + ":" + l.path
		return m.loadCompareDiffCmd(left, right, l), tag, i18n.T("%s ↔ working tree", shortHash(m.filesHash))
	case m.inCompareMode():
		// compareSides, not filesLeft/filesRight: a link compare's member may
		// have its own byte source, and the tag + cache key must follow it.
		left, right := m.compareSides(l)
		tag = "cmp:" + left.CacheTag() + ":" + right.CacheTag() + ":" + l.path
		return m.loadCompareDiffCmd(left, right, l), tag, m.filesContext
	case m.inShelfFiles():
		// Shelf mode: the frozen member (old) against the working file (new) —
		// the same two-ref compare the .-menu's compare-against-working-dir uses.
		subtitle := i18n.T("%s → working tree", m.filesShelfLabel)
		tag = "shelffile:" + m.filesShelfID + ":" + l.path
		return m.loadShelfMemberDiffCmd(m.filesShelfID, l.path, subtitle, tag), tag, subtitle
	}
	hash := m.lineHash(l)
	tag = "commit:" + hash + ":" + l.path
	return m.loadCommitDiffCmd(hash, l), tag, "@ " + m.filesContext
}

// moveListUnderFilesView moves the list side (the right column) by delta and
// fires its follow-live reload: the stash list when the file tree is showing a
// stash, otherwise the Commits list.
func (m Model) moveListUnderFilesView(delta int) (tea.Model, tea.Cmd) {
	if m.filesPreview != nil {
		// The preview owns the right column: vertical movement scrolls it instead
		// of the commit list (so filesHash can't change under a live preview). It is
		// a pager — move the top line directly (NOT contentPopup.move, which clamps
		// to a cursor range and is shared with the tree/help window) so every press,
		// keyboard or wheel, scrolls the viewport.
		p := m.filesPreview.p
		p.scrollBy(delta, m.filePreviewRowsCap())
		return m, nil
	}
	if m.stashView != nil {
		return m.moveStashUnderFilesView(delta)
	}
	return m.moveCommitUnderFilesView(delta)
}

// moveCommitUnderFilesView shifts the Commits selection by delta and fires
// the follow-live reload when it lands on a different commit.
func (m Model) moveCommitUnderFilesView(delta int) (tea.Model, tea.Cmd) {
	// Compare and shelf modes have no live commit list: shifting the selection
	// here would reassign filesHash and reload a plain commit view, discarding
	// the comparison / the shelved-commit tree. Guard at the chokepoint so
	// keyboard AND mouse (mouse.go calls this directly) are both locked out.
	if m.inCompareMode() || m.inShelfFiles() {
		return m, nil
	}
	// Pure-drop: while a per-commit files read is outstanding, ignore the move
	// entirely so held j/k is paced by read completion instead of queuing a read
	// per OS key-repeat (the files load is expensive on a large repo).
	if m.filesReadInflight {
		return m, nil
	}
	n := m.panelLen(panelCommits)
	s := m.sel[panelCommits] + delta
	if s > n-1 {
		s = n - 1
	}
	if s < 0 {
		s = 0
	}
	if s == m.sel[panelCommits] {
		return m, nil
	}
	m.sel[panelCommits] = s
	bi, ok := m.backingIndex(panelCommits)
	if !ok || m.commits[bi].Hash == m.filesHash {
		return m.maybeLoadMoreCommits() // nil cmd when not needed
	}
	m.filesHash = m.commits[bi].Hash
	m.filesReadInflight = true
	filesCmd := m.loadFilesListCmd(m.commits[bi]) // the list alone; the reviews once the cursor rests
	m, more := m.maybeLoadMoreCommits()
	if more != nil {
		return m, tea.Batch(filesCmd, more)
	}
	return m, filesCmd
}

// syncFilesViewToSelectedCommit reloads the tree for the currently selected
// commit when it differs from the one on display. Called when a commit filter
// commits with the files view open, so the tree follows the narrowed selection
// without the user having to press j/k.
func (m Model) syncFilesViewToSelectedCommit() (tea.Model, tea.Cmd) {
	if m.inCompareMode() {
		return m, nil // never follow-reload in compare mode (input-agnostic lock)
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok || m.commits[bi].Hash == m.filesHash {
		return m, nil
	}
	m.filesHash = m.commits[bi].Hash
	m.filesReadInflight = true
	return m, m.loadFilesForCmd(m.commits[bi])
}

// renderFilesView draws the commit files tree as one full-height left-column
// box; it replaces the Branches/Worktrees/Status panels while open. The border
// follows filesTreeFocused; the Commits panel blurs via panelFocused while the
// tree side is active.
func (m Model) renderFilesView(boxW, boxH int) string {
	p := m.filesView
	contentH := boxH - 2 // top/bottom border
	if contentH < 1 {
		contentH = 1
	}
	innerW := boxW - 4 // border (2) + horizontal padding (2)
	if innerW < 1 {
		innerW = 1
	}
	// The /-search input rides its own line beneath the title (not appended to
	// it) so a long commit subject can't truncate the query out of view.
	search := m.filesSearchLine()
	meta := m.filesMetaLineFor()
	title := m.filesTitle + m.prFreshnessSuffix()
	if m.inWorktreeFiles() {
		title = m.wtTitle()
	}
	rowsCap := m.filesRowsCap(boxH)

	vis := p.visible()
	// Window-then-build (filesGeometry): on a full tree the list is 10^4–10^5
	// rows, and building a winRow for every row each frame is O(n) (≈0.5s at
	// 40k rows). Only the slice the window can show is built. In cutoff/scroll
	// (one line per row) the math is byte-identical to building all rows
	// (windowStart clamps the same window either way); in wrap mode a row is
	// ≥1 lines, so the rowsCap-line window can never show rows outside
	// [sel-rowsCap, sel+rowsCap] and renderWindow's wrap windowing re-derives
	// the same span (see its output-identity argument).
	g := filesGeometry(vis, p.sel, rowsCap)
	anchor := g.anchor
	window := vis[g.s0:g.s1]
	wr := make([]winRow, len(window))
	s := st()
	for i, l := range window {
		prefix := "  "
		var st lipgloss.Style
		switch {
		case i == anchor:
			// Cursor highlight wins over heading style so the cursor stays
			// visible when it rests on a heading row.
			prefix = "> "
			st = s.selectedRow
		case l.heading:
			prefix = ""
			st = s.titleStyle
		case l.dim:
			st = s.noteDim
		}
		text := l.text
		// Directory headings carry the full path; a leaf dir (e.g. .../v3/ApiObject/)
		// would tail-truncate to look identical to its parent (.../v3/). Middle-elide
		// in cutoff mode so the distinguishing leaf (and the path's start) stay
		// visible; wrap and scroll modes already show the whole row, and the selected
		// row's reveal shows the untrimmed path. Pre-elided to fit so renderWindow
		// won't re-cut it.
		if l.heading && p.mode == modeCutoff {
			text = elidePath(l.text, innerW-lipgloss.Width(prefix))
		}
		// A shelved set's note row: the "└ date " lead stays whole and the
		// summary loses its middle, keeping its "(branch)" group (pre-cut to
		// fit, like the headings above).
		if l.shelfNote != "" && p.mode == modeCutoff {
			r := []rune(l.text)
			lead := string(r[:l.elideHead])
			text = lead + elideNoteSummary(string(r[l.elideHead:]), innerW-lipgloss.Width(prefix)-lipgloss.Width(lead))
		}
		// An open merge preview badges its file rows with the notes gathered
		// along the branch. Painted here rather than baked into l.text so the
		// badge tracks a counts refresh with no rebuild, and so the `/` filter
		// (which matches l.text) never matches a file by its note count.
		elide := l.path != "" && !l.heading && l.noteID == ""
		if m.filesPreviewSet != nil && l.path != "" {
			text += noteBadgeGroups(m.filesPreviewCounts[l.path], m.filesPreviewGroups[l.path])
		} else if l.noteScope != "" && m.filesCommitBadges() {
			// A commit's Range review row: every note of the scope here.
			text += noteBadge(scopeNoteCount(m.noteCounts, m.filesHash, l.noteScope))
		} else if l.notedPath != "" && m.filesCommitBadges() {
			// A commit's Notes row: the notes written outside any range; the
			// path before the badge loses its middle.
			text, elide = text+noteBadge(m.noteCounts.PlainByCommitPath[m.filesHash+":"+l.notedPath]), true
		} else if l.path != "" && m.filesCommitBadges() {
			// A commit's files: the notes anchored on the file AT this commit,
			// written outside any range — a range review's are its row's.
			text += noteBadge(m.noteCounts.PlainByCommitPath[m.filesHash+":"+l.path])
		}
		// A file row cuts the middle of its path, never the name (headings
		// were pre-elided above); a review row is prose and cuts at its end.
		wr[i] = winRow{text: prefix + text, style: st, elide: elide, elideHead: len([]rune(prefix))}
	}

	lines := make([]string, 0, contentH)
	pos := ""
	if len(vis) > rowsCap { // no hint line to carry it: where the cursor is
		pos = fmt.Sprintf("%d/%d", p.sel+1, len(vis))
	}
	lines = append(lines, titleWithRight(title, pos, innerW, false))
	if meta != "" {
		// Its own line, directly under the title: a long subject truncates the
		// title, and the date must not be the casualty of that.
		lines = append(lines, padRight(truncate(meta, innerW), innerW))
	}
	if search != "" {
		lines = append(lines, padRight(truncate(search, innerW), innerW))
	}
	if len(vis) == 0 {
		lines = append(lines, padRight(truncate(i18n.T("  (no match)"), innerW), innerW))
	} else {
		lines = append(lines, m.renderFileRows(wr, vis, g, innerW, rowsCap)...)
	}
	for len(lines) < contentH {
		lines = append(lines, padRight("", innerW))
	}

	style := s.bluredPanel
	if m.filesTreeFocused {
		style = s.focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}
