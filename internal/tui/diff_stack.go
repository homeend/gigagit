package tui

import (
	"context"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

// The STACKED diff view (design doc §5.2): one diff view showing EVERY file of
// the list it was opened from, each file a header line with its diff below,
// in one continuous scroll. `S` toggles it; the preference is machine-local
// (promptstate) and independent of the web's own (R7).
//
// The whole stack lives in ONE line stream, because everything the single-file
// view already does — the cursor, the display-row layout, wrap/scroll modes,
// the change-block jumps, the in-view search — is written against v.lines and
// v.disp. So a stacked view is not a second surface: it is the same surface
// with a stream that interleaves per-file header and placeholder lines between
// the files' rows, and with the per-file data (rows, syntax runs, load state)
// moved from the view's own fields onto one stackFile per file.

// lineKind says what a v.lines entry is. The zero value is a body line, so the
// single-file stream — which is nothing but body lines — is unchanged.
type lineKind uint8

const (
	lineBody   lineKind = iota // a diff row, or a fold separator (textdiff.Line as before)
	lineHeader                 // a stacked file's header row
	linePlace                  // a stacked file's one-line state: loading / binary / too large / error / no difference / conflict
	lineGap                    // the blank line that opens a stacked file
	lineRule                   // the rule under a stacked file's header
)

// diffLine is one logical line of the diff stream. It embeds textdiff.Line, so
// every .Row / .Fold read in the view compiles unchanged and the pure textdiff
// leaf learns nothing about stacks. file and kind mean something only while the
// view is stacked (file is the index into diffStack.files).
type diffLine struct {
	textdiff.Line
	file int
	kind lineKind
}

// isBody reports whether this line carries a real aligned row — the lines the
// cursor's ROW is read from, the search matches, and a selection copies.
func (l diffLine) isBody() bool { return l.kind == lineBody && l.Fold == 0 }

// isStop reports whether the cursor may rest here: body rows and file headers,
// never a fold separator or a placeholder. A header is a stop deliberately —
// a FOLDED file has nothing but its header, and `-` unfolds the cursor's file.
func (l diffLine) isStop() bool {
	return (l.kind == lineBody && l.Fold == 0) || l.kind == lineHeader
}

// wrapLines lifts a pure textdiff stream into the view's stream (one file,
// all body lines) — what the single-file view's rebuild produces.
func wrapLines(ls []textdiff.Line) []diffLine {
	out := make([]diffLine, len(ls))
	for i, l := range ls {
		out[i] = diffLine{Line: l}
	}
	return out
}

// stackLoad is one file's place in the lazy-load lifecycle (design §6.2).
type stackLoad uint8

const (
	stackIdle    stackLoad = iota // never requested
	stackLoading                  // a loader Cmd is out for it
	stackLoaded                   // d holds its diff
	stackStale                    // loaded, but a working-tree refresh says re-read it (d still shows meanwhile)
)

// stackCollapseOver is the file count above which a stack opens folded
// (design R2): past it, painting every file's diff is neither wanted nor
// affordable, so only the file the user opened is unfolded.
const stackCollapseOver = 100

// stackFile is one file of a stack — the TUI twin of the web's slot.
//
// d is the per-file view one of the ORDINARY single-file loaders built: the
// stack reuses them verbatim (loadCommitDiffCmd, loadCompareDiffCmd,
// loadStatusDiffCmd, …) and keeps their result here. It never renders d; it
// splices d's rows into its own stream, and reads d's syntax runs, binary /
// too-large / error state for this file's lines.
type stackFile struct {
	path, oldPath, status string
	line                  contentLine      // files-view source: the row its loader takes
	fs                    model.FileStatus // Status/Staged source: the entry its loader takes
	conflict              bool             // unmerged: header + "open the resolver", never fetched
	collapsed             bool
	load                  stackLoad
	d                     *diffView
	add, del              int
	counted               bool // add/del are known (from numstat, or from the rows on load)
	bin                   bool // numstat says binary
	start                 int  // index of this file's FIRST line in v.lines (its blank line; the header on the first file)
	hdr                   int  // index of this file's header line (stamped by spliceStack)
}

// diffStack is the open stack: which list it came from and its files.
// gen tags every async message it starts, so a rebuilt stack (S, a filter
// change, a working-tree refresh) drops the old stack's answers.
type diffStack struct {
	gen      int
	src      diffNavKind // diffNavTree | diffNavStatus | diffNavStaged
	staged   bool        // status source: the Staged section rather than the Files one
	files    []stackFile
	inflight int // loader Cmds currently out (capped at stackMaxInflight)
	// land is the cursor placement the stack OWES once a file it just sent for
	// has arrived: a }/{ step into a file whose diff (or whose notes) are not
	// here yet, a gg:// link or a steer naming a line inside it. It cannot be
	// done at key time — the lines it names do not exist yet — so it is parked,
	// exactly like the single-file view's }/{ landing.
	land *stackLanding
	// hunt is the ] / [ step a file owes once it arrives: the search ran out
	// of hits in that direction, so the next file it has not searched was
	// unfolded and sent for. Separate from `land` because it waits on the DIFF
	// alone (a note landing also waits on stackNotesMsg), and mutually
	// exclusive with it — setting one clears the other.
	hunt *stackHunt
}

// stackHunt is a parked search step: file `file` was unfolded (and sent for if
// it had never been fetched) on behalf of a ] (dir>0) / [ (dir<0). When its
// lines arrive the search is re-found and the cursor lands on that file's
// first (dir>0) / last (dir<0) hit — and when it holds none, the step hands on
// to the next unsearched file, one round trip at a time (design D2).
type stackHunt struct{ file, dir int }

// stackLanding is one parked cursor placement inside a stack. dir != 0 means
// "this file's FIRST (dir>0) / LAST (dir<0) note"; no > 0 means "this exact
// line on this side". A landing is dropped the moment its file is dropped
// (a new generation rebuilds the stack and the pointer with it).
type stackLanding struct {
	file int
	dir  int
	side model.NoteSide
	no   int
}

// spliceStack rebuilds v.lines / v.blocks from the stack's files: per file a
// header line, then its body — its rows in the current fold mode, or ONE
// placeholder line when there is nothing to show (not loaded yet, binary, too
// large, failed, conflicted, or no content difference), or nothing at all when
// the file is folded.
//
// v.blocks (the change-block jump targets) become the HEADER indices, which is
// what makes n/p step file to file with no new navigation code: every jump,
// wrap and ordinal in the single-file view is expressed in terms of blocks.
func (v *diffView) spliceStack() {
	v.lines, v.blocks = v.lines[:0], v.blocks[:0]
	for i := range v.stk.files {
		f := &v.stk.files[i]
		f.start = len(v.lines)
		// A blank line opens each file and a rule closes its header, so the
		// files read as separate blocks instead of one unbroken column. The
		// first file skips the blank — there is nothing above it to separate.
		if i > 0 {
			v.lines = append(v.lines, diffLine{file: i, kind: lineGap})
		}
		f.hdr = len(v.lines)
		v.blocks = append(v.blocks, f.hdr)
		v.lines = append(v.lines, diffLine{file: i, kind: lineHeader})
		v.lines = append(v.lines, diffLine{file: i, kind: lineRule})
		if f.collapsed {
			continue
		}
		var body []textdiff.Line
		if d := f.d; !f.conflict && d != nil && d.err == nil && !d.binary && !d.tooLarge {
			if v.partial {
				body, _ = textdiff.Collapse(d.full, d.fullBlocks, diffContext)
			} else {
				body = textdiff.Expand(d.full)
			}
		}
		if len(body) == 0 {
			v.lines = append(v.lines, diffLine{file: i, kind: linePlace})
			continue
		}
		for _, l := range body {
			v.lines = append(v.lines, diffLine{Line: l, file: i})
		}
	}
}

// curFile is the index of the file the cursor is in (0 on an empty stack).
// Every per-file action on a stack reads it: the title, the header count, the
// fold keys, h/b/e, and the copy rows.
func (v *diffView) curFile() int {
	if v.stk == nil || v.curLine < 0 || v.curLine >= len(v.lines) {
		return 0
	}
	return v.lines[v.curLine].file
}

// stackFileAt returns the stack file logical line li belongs to.
func (v *diffView) stackFileAt(li int) (stackFile, bool) {
	if v.stk == nil || li < 0 || li >= len(v.lines) {
		return stackFile{}, false
	}
	i := v.lines[li].file
	if i < 0 || i >= len(v.stk.files) {
		return stackFile{}, false
	}
	return v.stk.files[i], true
}

// toksFor is the syntax runs painting line li's two sides. Stacked, they come
// from the line's OWN file: runs are indexed by SOURCE LINE NUMBER, so file
// B's line 5 would otherwise be painted with file A's tokens.
func (v *diffView) toksFor(li int) (old, nw [][]syntax.Tok) {
	if v.stk == nil {
		return v.oldTok, v.newTok
	}
	if f, ok := v.stackFileAt(li); ok && f.d != nil {
		return f.d.oldTok, f.d.newTok
	}
	return nil, nil
}

// gutter is the line-number column width. Stacked, it is the widest over the
// files LOADED so far, so every file's numbers line up; a late load that needs
// a wider gutter relayouts the whole stack (the load path relayouts anyway).
func (v *diffView) gutter() int {
	if v.stk == nil {
		return gutterWidth(v.full)
	}
	g := 3
	for i := range v.stk.files {
		if d := v.stk.files[i].d; d != nil {
			if w := gutterWidth(d.full); w > g {
				g = w
			}
		}
	}
	return g
}

// fileLineRange is the half-open-free [first, last] logical-line range file i
// owns, headers included. It is how per-file actions stay inside their file.
func (v *diffView) fileLineRange(i int) (lo, hi int) {
	if v.stk == nil || i < 0 || i >= len(v.stk.files) {
		return 0, len(v.lines) - 1
	}
	lo = v.stk.files[i].start
	hi = len(v.lines) - 1
	if i+1 < len(v.stk.files) {
		hi = v.stk.files[i+1].start - 1
	}
	return lo, hi
}

// stackMaxInflight caps the loader Cmds a stack has out at once (design §6.2):
// enough to keep reading ahead, few enough that a 500-file stack never floods
// the repo gate with reads the user scrolled past long ago.
const stackMaxInflight = 3

// stackFileMsg delivers ONE file's diff into the stack that asked for it. gen
// gates it: a stack rebuilt meanwhile (S, a working-tree refresh) has a new
// generation and drops every answer owed to the old one. It is deliberately
// NOT diffMsg — that handler replaces the whole view (*dv = *msg.view), which
// would throw the stack away.
type stackFileMsg struct {
	gen, idx int
	view     *diffView
}

// stackNotesMsg delivers ONE file's resolved review notes into the stack that
// asked for them. Like stackFileMsg it is deliberately not the single-file
// message: notesLoadedMsg is gated on the view's one diffTag and replaces the
// WHOLE view's notes, so in a stack it would drop every answer but the last
// and write that one over file 0.
type stackNotesMsg struct {
	gen, idx int
	notes    []domain.ResolvedNote
	err      error
}

// stackNotesCmd resolves file idx's review notes off the UI thread, against
// the address ITS OWN loader stamped (d.noteAddr / d.previewSet) and its own
// rows — exactly what the single-file view does, once per file. A file with no
// address (every two-sided compare) resolves nothing, so notes stay inert in a
// stack exactly where they are inert single-file.
func (m Model) stackNotesCmd(gen, idx int, d *diffView) tea.Cmd {
	if m.svc == nil || d == nil || d.noteAddr.Path == "" {
		return nil
	}
	svc, addr, set, rows := m.svc, d.noteAddr, d.previewSet, d.full
	return func() tea.Msg {
		dd := domain.Diff{Result: textdiff.Result{Rows: rows}}
		if set != nil {
			ns, err := svc.PreviewNotesFor(context.Background(), *set, addr.Path, dd)
			return stackNotesMsg{gen: gen, idx: idx, notes: ns, err: err}
		}
		ns, err := svc.NotesFor(context.Background(), addr, dd)
		return stackNotesMsg{gen: gen, idx: idx, notes: ns, err: err}
	}
}

// stackAnchor is a place in the stream that survives a re-splice: which file,
// and how far into it. Line indexes themselves do not survive — a file loading
// above the viewport shifts every index below it.
type stackAnchor struct {
	file   int
	inFile int // -1 = the file's header; else the offset from its first body line
	sub    int // display rows below that line's first row (a wrapped line owns several)
}

// anchorAt describes logical line li as a stack anchor.
func (v *diffView) anchorAt(li int) stackAnchor {
	if v.stk == nil || li < 0 || li >= len(v.lines) {
		return stackAnchor{inFile: -1}
	}
	f := v.lines[li].file
	if f < 0 || f >= len(v.stk.files) {
		return stackAnchor{inFile: -1}
	}
	// Measured from the file's FIRST line, so the blank line and the rule
	// shift nothing when a stream is re-spliced.
	return stackAnchor{file: f, inFile: li - v.stk.files[f].start}
}

// lineAt maps an anchor back to a logical line in the CURRENT stream, clamped
// into the file it names (a file that folded, or shrank, lands on its header).
func (v *diffView) lineAt(a stackAnchor) int {
	if v.stk == nil || len(v.lines) == 0 {
		return 0
	}
	f := a.file
	if f < 0 || f >= len(v.stk.files) {
		return 0
	}
	lo, hi := v.fileLineRange(f)
	if a.inFile < 0 {
		return v.stk.files[f].hdr
	}
	li := lo + a.inFile
	if li > hi {
		li = hi
	}
	if li < lo {
		li = lo
	}
	return li
}

// wantLoads picks the files to request now: never folded, never a conflict,
// idle (or stale after a refresh), with their header inside [lo, hi] — the
// viewport widened by about two screens — nearest to `at` first, and only as
// many as the in-flight cap still allows. Pure, so the queue is a table test.
func wantLoads(files []stackFile, lo, hi, at, inflight int) []int {
	room := stackMaxInflight - inflight
	if room <= 0 {
		return nil
	}
	type cand struct{ idx, dist int }
	var cs []cand
	for i, f := range files {
		if f.collapsed || f.conflict || (f.load != stackIdle && f.load != stackStale) {
			continue
		}
		if f.start < lo || f.start > hi {
			continue
		}
		d := f.start - at
		if d < 0 {
			d = -d
		}
		cs = append(cs, cand{i, d})
	}
	sort.SliceStable(cs, func(a, b int) bool {
		if cs[a].dist != cs[b].dist {
			return cs[a].dist < cs[b].dist
		}
		// A tie is one file above and one below the reading position: prefer
		// the one BELOW, which is where the eye is going.
		return files[cs[a].idx].start > files[cs[b].idx].start
	})
	if len(cs) > room {
		cs = cs[:room]
	}
	out := make([]int, len(cs))
	for i, c := range cs {
		out[i] = c.idx
	}
	return out
}

// stackWindow is the logical-line range the loader looks at: what is on screen,
// widened by two screens each way (design §6.2), plus the line the viewport
// starts at, which is what "nearest" is measured from.
func (v *diffView) stackWindow(body int) (lo, hi, at int) {
	if len(v.disp) == 0 {
		return 0, len(v.lines) - 1, 0
	}
	top := v.disp[clampInt(v.offset, 0, len(v.disp)-1)].line
	bot := v.disp[clampInt(v.offset+body-1, 0, len(v.disp)-1)].line
	return top - 2*body, bot + 2*body, top
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// pumpStack starts the loads the current viewport calls for. It is called
// after every key, every arrival and every resize while a stack is open: the
// queue is not a background worker, it is a function of where the user is.
func (m Model) pumpStack() (Model, tea.Cmd) {
	v := m.diffLayer()
	if v == nil || v.stk == nil {
		return m, nil
	}
	lo, hi, at := v.stackWindow(m.diffBodyRows())
	picks := wantLoads(v.stk.files, lo, hi, at, v.stk.inflight)
	if len(picks) == 0 {
		return m, nil
	}
	var cmds []tea.Cmd
	for _, i := range picks {
		f := &v.stk.files[i]
		cmd := m.stackFileCmd(*f)
		if cmd == nil {
			f.load = stackLoaded // nothing can load it: leave its placeholder
			continue
		}
		f.load = stackLoading
		v.stk.inflight++
		cmds = append(cmds, stackCmd(v.stk.gen, i, cmd))
	}
	return m, tea.Batch(cmds...)
}

// stackFileCmd is the ordinary single-file loader for one stack file, chosen
// by the list the stack came from.
func (m Model) stackFileCmd(f stackFile) tea.Cmd {
	switch m.diffLayer().stk.src {
	case diffNavTree:
		cmd, _, _ := m.treeFileLoad(f.line)
		return cmd
	case diffNavStatus:
		return m.loadStatusDiffCmd(f.fs, false)
	case diffNavStaged:
		return m.loadStatusDiffCmd(f.fs, true)
	}
	return nil
}

// stackCmd re-labels a single-file loader's diffMsg as this stack file's
// answer, so the stack's own handler sees it and the diffMsg handler (which
// replaces the entire view) never does.
func stackCmd(gen, idx int, c tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		if d, ok := c().(diffMsg); ok {
			return stackFileMsg{gen: gen, idx: idx, view: d.view}
		}
		return nil
	}
}

// applyStackFile lands one file's diff: its rows replace the placeholder, its
// counts fill in if numstat has not already named them, and the cursor and the
// viewport keep the lines they were on — a file loading ABOVE the viewport
// shifts every index below it, so both are re-found by (file, line in file).
func (m Model) applyStackFile(msg stackFileMsg) (Model, tea.Cmd) {
	v := m.diffLayer()
	if v == nil || v.stk == nil || msg.gen != v.stk.gen || msg.idx < 0 || msg.idx >= len(v.stk.files) {
		return m, nil
	}
	f := &v.stk.files[msg.idx]
	if f.load == stackLoading && v.stk.inflight > 0 {
		v.stk.inflight--
	}
	f.d, f.load = msg.view, stackLoaded
	if !f.counted && msg.view != nil {
		f.add, f.del = countRows(msg.view.full)
		f.counted = true
	}
	body := m.diffBodyRows()
	cur := v.anchorAt(v.curLine)
	topLine := 0
	if len(v.disp) > 0 {
		topLine = v.disp[clampInt(v.offset, 0, len(v.disp)-1)].line
	}
	top := v.anchorAt(topLine)
	if len(v.lineStart) > topLine {
		top.sub = v.offset - v.lineStart[topLine]
	}
	v.rebuildLines()
	v.curLine = v.lineAt(cur)
	tl := v.lineAt(top)
	if tl < len(v.lineStart) {
		v.offset = v.lineStart[tl] + top.sub
	}
	// AFTER the remap: refindAfterRebuild measures from v.curLine, and until
	// the line above is run that index still names the OLD stream.
	v.refindAfterRebuild()
	v.scroll(0, body)
	v.syncStackTitle()
	// A ] / [ that stepped into this file lands now that its rows exist.
	m, hcmd := m.drainStackHunt(msg.idx, body)
	// This file's own review notes follow its diff: they resolve against the
	// rows that just arrived, so they cannot be asked for any earlier.
	return m, tea.Batch(hcmd, m.stackNotesCmd(v.stk.gen, msg.idx, f.d))
}

// countRows is a file's +adds / −dels from its aligned rows — the count for
// sources git cannot numstat (a shelf member, an entry, a preview).
func countRows(rows []textdiff.Row) (add, del int) {
	for _, r := range rows {
		switch r.Kind {
		case textdiff.Add:
			add++
		case textdiff.Del:
			del++
		case textdiff.Changed:
			add, del = add+1, del+1
		}
	}
	return add, del
}

// syncStackTitle points the view's title at the cursor's file, which is how
// every per-file action (h, b, e, the copy rows, export, a bookmark) keeps
// acting on the file being read without learning about stacks at all.
func (v *diffView) syncStackTitle() {
	if v.stk == nil || len(v.stk.files) == 0 {
		return
	}
	v.title = v.stk.files[v.curFile()].path
}

// stackStatMsg carries one numstat read for the stack that asked for it.
type stackStatMsg struct {
	gen   int
	stats []model.DiffStat
}

// stackStatCmd asks git for the whole stack's +/− counts in ONE read, wherever
// git has a tree pair to count: a commit, a commit↔commit compare, or a
// working-tree section. Sources with no such pair — a shelf member, a bookmark,
// a preview, a live endpoint — return nil, and their counts come from the rows
// as each file loads (design §7). It is asked only while a stack is open, so
// the listings and the status poll pay nothing for it.
func (m Model) stackStatCmd(stk *diffStack) tea.Cmd {
	svc := m.svc
	if svc == nil || stk == nil {
		return nil
	}
	gen := stk.gen
	switch {
	case stk.src == diffNavStatus || stk.src == diffNavStaged:
		cached := stk.staged
		return func() tea.Msg {
			stats, err := svc.DiffStat(context.Background(), model.DiffSpec{Cached: cached})
			if err != nil {
				return nil
			}
			return stackStatMsg{gen: gen, stats: stats}
		}
	case m.inCompareMode():
		l, r := m.filesLeft, m.filesRight
		if l.Kind() != model.EndpointCommit || r.Kind() != model.EndpointCommit {
			return nil // a live or non-commit side: no immutable pair to count
		}
		rev := l.Hash() + ".." + r.Hash()
		return func() tea.Msg {
			stats, err := svc.DiffStat(context.Background(), model.DiffSpec{Rev: rev})
			if err != nil {
				return nil
			}
			return stackStatMsg{gen: gen, stats: stats}
		}
	case stk.src == diffNavTree && m.filesHash != "" && !m.inShelfFiles() && !m.inFullTree():
		hash := m.filesHash
		return func() tea.Msg {
			stats, err := svc.CommitStat(context.Background(), hash)
			if err != nil {
				return nil
			}
			return stackStatMsg{gen: gen, stats: stats}
		}
	}
	return nil
}

// applyStackStats fills the headers' counts. git's numstat is authoritative:
// the rows only fill in files it did not name (an untracked file is not in a
// working-tree numstat at all).
func (m Model) applyStackStats(msg stackStatMsg) Model {
	v := m.diffLayer()
	if v == nil || v.stk == nil || msg.gen != v.stk.gen {
		return m
	}
	by := make(map[string]model.DiffStat, len(msg.stats))
	for _, st := range msg.stats {
		by[st.Path] = st
	}
	for i := range v.stk.files {
		st, ok := by[v.stk.files[i].path]
		if !ok {
			continue
		}
		f := &v.stk.files[i]
		f.add, f.del, f.bin, f.counted = st.Added, st.Deleted, st.Binary, true
	}
	return m
}
