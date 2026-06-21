package tui

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/model"
)

// point is a cell coordinate on the rendered screen (x = column, y = row).
type point struct{ x, y int }

// layoutGeom is the panel geometry renderInterface draws with. boxH holds each
// panel's box height under the current layout; a panel missing from the map
// (or 0) is not visible at this terminal size. pos holds each visible panel's
// top-left corner (the header occupies screen row 0).
type layoutGeom struct {
	w, h, bodyH   int
	leftW, rightW int
	boxH          map[panel]int
	pos           map[panel]point
}

// layout computes panel geometry for the current terminal size. It is the
// single source of truth shared by renderInterface and the paging keys, so
// rendering and navigation can never disagree about a panel's viewport.
func (m Model) layout() layoutGeom {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	bodyH := h - 3
	if bodyH < 6 {
		bodyH = 6
	}
	g := layoutGeom{w: w, h: h, bodyH: bodyH, boxH: map[panel]int{}, pos: map[panel]point{}}

	// Narrow terminals: a single commits column.
	if w < 40 {
		g.rightW = w
		g.boxH[panelCommits] = bodyH
		g.pos[panelCommits] = point{0, 1}
		return g
	}

	leftW := w / 3
	if leftW < 16 {
		leftW = 16
	}
	if leftW > w-24 {
		leftW = w - 24
	}
	g.leftW, g.rightW = leftW, w-leftW

	// Left column: the Branches/Worktrees tab slot, the middle slot (Files OR
	// Tags — whichever is active), and Staged. Each bordered box needs >=3 rows;
	// a short terminal drops Staged. The inactive top tab, the inactive middle
	// tab, and a dropped Staged get no boxH entry (0 ⇒ hidden everywhere;
	// panelAt/render skip boxH<=0) but keep their state. Keying the middle box on
	// m.middleTab() (not always panelFiles) means page steps, windowing, and
	// mouse hit-testing all resolve to whichever tab is on screen.
	mid := m.middleTab()
	if bodyH >= 12 {
		h1 := bodyH / 3
		h2 := bodyH / 3
		g.boxH[m.activeLeftTab] = h1
		g.boxH[mid] = h2
		g.boxH[panelStaged] = bodyH - h1 - h2
		g.pos[m.activeLeftTab] = point{0, 1}
		g.pos[mid] = point{0, 1 + h1}
		g.pos[panelStaged] = point{0, 1 + h1 + h2}
	} else {
		h1 := bodyH / 2
		g.boxH[m.activeLeftTab] = h1
		g.boxH[mid] = bodyH - h1
		g.pos[m.activeLeftTab] = point{0, 1}
		g.pos[mid] = point{0, 1 + h1}
	}
	// Maximize: a pinned left panel takes the whole left column; the others hide
	// (boxH/pos deleted ⇒ boxH==0, which render/hit-test/paging all skip). Driven
	// off the logical left-panel set so a pin never zeroes a panel reachability
	// still depends on. Commits geometry is untouched. The Contains guard keeps
	// the block correct if the pinned panel ever falls out of the visible set
	// (e.g. a future writer switches the active tab without re-pinning): rather
	// than match nothing and delete every left box (blank column), fall through
	// to the normal split.
	if m.leftMaxed && slices.Contains(m.leftColumnPanels(), m.leftMax) {
		for _, p := range m.leftColumnPanels() {
			if p == m.leftMax {
				g.boxH[p] = bodyH
				g.pos[p] = point{0, 1}
			} else {
				delete(g.boxH, p)
				delete(g.pos, p)
			}
		}
	}

	g.boxH[panelCommits] = bodyH
	g.pos[panelCommits] = point{leftW, 1}
	return g
}

// panelRowsCap is how many data rows panel p can display right now (0 when the
// layout hides it). Mirrors renderPanel: box height minus borders (2) minus
// the label line (1).
func (m Model) panelRowsCap(p panel) int {
	n := m.layout().boxH[p] - 3
	if n < 0 {
		n = 0
	}
	return n
}

// pageStep is the pgup/pgdown jump: 25% of the focused panel's viewport,
// at least 1 row.
func (m Model) pageStep() int {
	s := m.panelRowsCap(m.focus) / 4
	if s < 1 {
		s = 1
	}
	return s
}

// sortMode is a panel's display order. Cycled by the `o` key.
type sortMode int

const (
	sortDefault sortMode = iota // git's emission order
	sortNameAsc
	sortNameDesc
	sortDateAsc
	sortDateDesc
	sortModeCount
)

// String is the label suffix; empty for the default order.
func (s sortMode) String() string {
	switch s {
	case sortNameAsc:
		return "name↑"
	case sortNameDesc:
		return "name↓"
	case sortDateAsc:
		return "date↑"
	case sortDateDesc:
		return "date↓"
	}
	return ""
}

// panelList is the per-panel contract behind generic filtering and sorting.
// Each panel implements its own semantics for name and date; the pipeline
// never inspects concrete types. Row(i) must be the display text of backing
// element i (the existing row builders are 1:1 with their slices).
type panelList interface {
	Len() int
	Row(i int) string  // display text (also the filter-match target, unless haystacker)
	Name(i int) string // what "sort by name" means for THIS panel
	Date(i int) int64  // what "sort by date" means for THIS panel (unix; 0 = unknown)
	Key(i int) string  // stable identity of backing element i (mark survival)
}

// haystacker is an optional panelList capability: a search-match string distinct
// from the displayed Row(i). The Commits panel uses it so the trimmed, id-less
// row can still be searched by full commit id or full branch name.
type haystacker interface{ Haystack(i int) string }

// textRevealer is an optional panelList capability: a compact text-only reveal
// string for the tooltip (no positional decoration like the commit graph, no
// fixed-width padding). When present, the tooltip draws this instead of the raw
// display row once it has decided the row is reveal-worthy.
type textRevealer interface{ TextReveal(i int) string }

// fullRower is an optional panelList capability: a parallel row whose content is
// fully un-elided, shown by the reveal tooltip when it differs from Row(i).
type fullRower interface{ Full(i int) string }

type branchList struct {
	items []model.Branch
	rows  []string
}

func (l branchList) Len() int          { return len(l.items) }
func (l branchList) Row(i int) string  { return l.rows[i] }
func (l branchList) Name(i int) string { return l.items[i].Name }
func (l branchList) Date(i int) int64  { return l.items[i].UnixTime }
func (l branchList) Key(i int) string  { return l.items[i].Name }

type remoteBranchList struct {
	items []model.RemoteBranch
	rows  []string
}

func (l remoteBranchList) Len() int          { return len(l.items) }
func (l remoteBranchList) Row(i int) string  { return l.rows[i] }
func (l remoteBranchList) Name(i int) string { return l.items[i].Name }
func (l remoteBranchList) Date(i int) int64  { return l.items[i].UnixTime }
func (l remoteBranchList) Key(i int) string  { return l.items[i].Name }

type worktreeList struct {
	items []model.Worktree
	rows  []string
	times map[string]int64 // HEAD sha -> committer time
}

func (l worktreeList) Len() int         { return len(l.items) }
func (l worktreeList) Row(i int) string { return l.rows[i] }
func (l worktreeList) Name(i int) string {
	if b := l.items[i].Branch; b != "" {
		return b
	}
	return l.items[i].Path // detached/bare fall back to the path
}
func (l worktreeList) Date(i int) int64 { return l.times[l.items[i].Head] }
func (l worktreeList) Key(i int) string { return l.items[i].Path }

type tagList struct {
	items []model.Tag
	rows  []string
}

func (l tagList) Len() int          { return len(l.items) }
func (l tagList) Row(i int) string  { return l.rows[i] }
func (l tagList) Name(i int) string { return l.items[i].Name }
func (l tagList) Date(i int) int64  { return 0 } // no per-tag date in v1; git default order is newest-first
func (l tagList) Key(i int) string  { return l.items[i].Name }

type statusList struct {
	files []model.FileStatus
	rows  []string
	root  string
	mtime map[int]int64 // dedupes os.Stat within one sort pass; 0 = unknown (sorts last)
}

func (l statusList) Len() int          { return len(l.files) }
func (l statusList) Row(i int) string  { return l.rows[i] }
func (l statusList) Name(i int) string { return l.files[i].Path }
func (l statusList) Key(i int) string  { return l.files[i].Path }
func (l statusList) Date(i int) int64 {
	if t, ok := l.mtime[i]; ok {
		return t
	}
	var t int64
	if fi, err := os.Stat(filepath.Join(l.root, l.files[i].Path)); err == nil {
		t = fi.ModTime().Unix()
	}
	// value receiver: writing through the shared map backing store is intentional
	l.mtime[i] = t
	return t
}

type commitList struct {
	items  []model.Commit
	m      *Model // source for lazy per-index row/full/text/haystack (built on demand, not all-n per frame)
	identW int    // shared identity-column width
}

func (l commitList) Len() int { return len(l.items) }
func (l commitList) Row(i int) string {
	if l.m == nil {
		return ""
	}
	return l.m.commitIdentRowAt(i, l.identW, false)
}
func (l commitList) Name(i int) string { return l.items[i].Subject }
func (l commitList) Date(i int) int64  { return l.items[i].UnixTime }
func (l commitList) Key(i int) string  { return l.items[i].Hash }

// Haystack decouples the filter-match text from the (trimmed, id-less) display
// row so id-prefix and full-branch-name searches keep working. Full supplies the
// untrimmed-identity row the reveal tooltip shows. Both are computed per-index on
// demand (only when a filter is active / the tooltip reads one row) rather than
// materialized for the whole feed every frame.
func (l commitList) Haystack(i int) string {
	if l.m == nil || i >= len(l.items) {
		return ""
	}
	return l.m.commitHaystackAt(i)
}
func (l commitList) Full(i int) string {
	if l.m == nil || i >= len(l.items) {
		return ""
	}
	return l.m.commitIdentRowAt(i, l.identW, true)
}

// TextReveal supplies the compact text-only reveal (branch + subject, no graph
// glyphs, no fixed-width padding) the tooltip draws once it has decided the row
// is reveal-worthy. See textRevealer.
func (l commitList) TextReveal(i int) string {
	if l.m == nil || i >= len(l.items) {
		return ""
	}
	return l.m.commitTextRevealAt(i)
}

// inFilesPanel reports whether f belongs in the Files panel: any working-tree
// change (Unstaged side), including untracked and conflicts.
func inFilesPanel(f model.FileStatus) bool {
	return f.Kind == model.KindUntracked || (f.Unstaged != '.' && f.Unstaged != 0)
}

// inStagedPanel reports whether f belongs in the Staged panel: an index change.
// Untracked and unmerged (conflict) entries are excluded.
func inStagedPanel(f model.FileStatus) bool {
	if f.Kind == model.KindUntracked || f.Kind == model.KindUnmerged {
		return false
	}
	return f.Staged != '.' && f.Staged != 0 && f.Staged != '?'
}

// memberOf reports whether backing element i of panel p is shown there. Only the
// Files/Staged split filters; every other panel shows all of its rows.
func (m Model) memberOf(p panel, i int) bool {
	switch p {
	case panelFiles:
		return inFilesPanel(m.status.Files[i])
	case panelStaged:
		return inStagedPanel(m.status.Files[i])
	}
	return true
}

// listFor builds panel p's panelList from the current model snapshot.
func (m Model) listFor(p panel) panelList {
	switch p {
	case panelBranches:
		return branchList{items: m.branches, rows: m.branchRows()}
	case panelRemotes:
		return remoteBranchList{items: m.remoteBranches, rows: m.remoteRows()}
	case panelWorktrees:
		return worktreeList{items: m.worktrees, rows: m.worktreeRows(), times: m.headTimes}
	case panelTags:
		return tagList{items: m.tags, rows: m.tagRows()}
	case panelFiles, panelStaged:
		// Both file panels back onto the FULL status slice; panelView's
		// membership filter selects each panel's subset, so backingIndex keeps
		// returning indices into m.status.Files for the action handlers.
		return statusList{files: m.status.Files, rows: m.statusRows(p), root: m.currentWorktree, mtime: map[int]int64{}}
	case panelCommits:
		return commitList{items: m.commits, m: &m, identW: m.commitIdentWidth()}
	}
	return commitList{}
}

// sortIndices orders backing indices in place under mode. sortDefault is a
// no-op. Ties and unknown comparisons keep backing order (stable).
func sortIndices(l panelList, mode sortMode, idx []int) {
	if mode == sortDefault {
		return
	}
	sort.SliceStable(idx, func(a, b int) bool { return viewLess(l, mode, idx[a], idx[b]) })
}

// viewLess orders two backing indices under mode. Unknown dates (0) sort last
// in BOTH directions so missing data never floats to the top.
func viewLess(l panelList, mode sortMode, a, b int) bool {
	switch mode {
	case sortNameAsc, sortNameDesc:
		na, nb := strings.ToLower(l.Name(a)), strings.ToLower(l.Name(b))
		if na == nb {
			return false
		}
		if mode == sortNameAsc {
			return na < nb
		}
		return na > nb
	case sortDateAsc, sortDateDesc:
		da, db := l.Date(a), l.Date(b)
		if da == 0 || db == 0 {
			return da != 0 && db == 0
		}
		if da == db {
			return false
		}
		if mode == sortDateAsc {
			return da < db
		}
		return da > db
	}
	return false
}

// filterActive reports whether panel p currently has a committed or in-progress
// filter query.
func (m Model) filterActive(p panel) bool {
	return p == m.filterPanel && m.filterQuery != ""
}

// panelView applies panel p's sort and filter, returning the display rows and
// the matching backing indices (display row n shows backing element idx[n]).
// It is the single source of truth for what a panel shows; selection, paging,
// clamping, rendering, and action keys all consume it.
// displayIndices applies panel p's membership filter, text filter, and sort,
// returning the backing indices in display order WITHOUT materializing any row
// strings. panelView builds rows on top of this; idx-only callers use it
// directly so they never pay for styling — important for the Commits panel,
// whose Row styling (graph window + identity token) is the per-frame hot cost.
func (m Model) displayIndices(p panel) (idx []int) {
	l := m.listFor(p)
	q := ""
	if m.filterActive(p) {
		q = strings.ToLower(m.filterQuery)
	}
	idx = make([]int, 0, l.Len())
	for i := 0; i < l.Len(); i++ {
		if !m.memberOf(p, i) {
			continue // Files/Staged split: each panel shows only its subset
		}
		if q != "" {
			// Prefer the cheap haystack; only fall back to Row(i) for panels that
			// don't implement haystacker. For commits Row(i) is now full styling
			// (graph window + identity), so calling it per row here would re-add
			// O(n) styling on every filtered frame.
			var text string
			if h, ok := l.(haystacker); ok {
				text = h.Haystack(i) // search hidden full id + branch name, not the trimmed row
			} else {
				text = l.Row(i)
			}
			if !strings.Contains(strings.ToLower(text), q) {
				continue
			}
		}
		idx = append(idx, i)
	}
	sortIndices(l, m.sortModes[p], idx)
	return idx
}

func (m Model) panelView(p panel) (rows []string, idx []int) {
	idx = m.displayIndices(p)
	l := m.listFor(p)
	rows = make([]string, len(idx))
	for n, i := range idx {
		rows[n] = l.Row(i)
	}
	return rows, idx
}

// backingIndex resolves panel p's current selection to an index into its
// backing slice, accounting for the view transforms. ok is false when the
// visible list is empty or the selection is out of range.
func (m Model) backingIndex(p panel) (int, bool) {
	idx := m.displayIndices(p)
	s := m.sel[p]
	if s < 0 || s >= len(idx) {
		return 0, false
	}
	return idx[s], true
}

// commitsLoadingGlyph marks the Commits title while a feed reload or page is in
// flight — on a huge repo a scope change or paging walk takes seconds, so the
// title shows it is working rather than appearing frozen.
const commitsLoadingGlyph = "⏳"

// panelLabel decorates a panel title with commit count (Commits panel only),
// active sort mode, and filter.
func (m Model) panelLabel(p panel, base string) string {
	if p == panelCommits {
		n := len(m.commits)
		if m.commitsExhausted {
			base += " " + strconv.Itoa(n)
		} else {
			base += " " + strconv.Itoa(n) + "+"
		}
		if m.commitsLoading {
			base += " " + commitsLoadingGlyph
		}
		if m.commitGraphIndexing {
			base += " (indexing…)"
		}
	}
	if s := m.sortModes[p].String(); s != "" {
		base += " ·" + s
	}
	if m.filterTyping && p == m.filterPanel {
		base += " /" + m.filterQuery + "█"
	} else if m.filterActive(p) {
		base += " /" + m.filterQuery
	}
	return base
}

// panelAt returns the panel whose box contains screen cell (x, y) under the
// current layout (border cells count as the panel). ok is false on the
// header/footer/status rows and any gap; panels the layout hides never match.
func (m Model) panelAt(x, y int) (panel, bool) {
	g := m.layout()
	for p := panel(0); p < panelCount; p++ {
		h := g.boxH[p]
		if h <= 0 {
			continue
		}
		w := g.leftW
		if p == panelCommits {
			w = g.rightW
		}
		pos := g.pos[p]
		if x >= pos.x && x < pos.x+w && y >= pos.y && y < pos.y+h {
			return p, true
		}
	}
	return 0, false
}

// panelRowAt maps screen row y inside panel p to an index into p's display
// rows (panelView order). ok is false on the border, the label line, and
// the padding below the last row. Uses the same windowStart the renderer
// uses, so the mapping cannot drift from what is on screen.
func (m Model) panelRowAt(p panel, y int) (int, bool) {
	g := m.layout()
	rowsCap := m.panelRowsCap(p)
	i := y - g.pos[p].y - 2 // top border + label line
	if i < 0 || i >= rowsCap {
		return 0, false
	}
	rows, _ := m.panelView(p)
	idx := windowStart(len(rows), rowsCap, m.sel[p]) + i
	if idx >= len(rows) {
		return 0, false
	}
	return idx, true
}
