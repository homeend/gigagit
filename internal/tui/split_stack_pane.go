package tui

import (
	"context"
	"errors"
	"sort"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// The STACKED file pane (spec 2026-10-10-review-overview-split §4.2): one
// unregistered document whose lines are every review file's content at the
// reviewed tip, headers between them, loaded lazily. Bands, the cursor and
// every anchor are per file through the stream table (split_stack.go).

// stackPane is the pane: the document, its stream, the loads in flight and
// the place a step owes once its file arrives.
type stackPane struct {
	docPane
	sv       *splitView
	stream   *fileStream
	gen      int64 // tags every load; a new pane drops an old one's answers
	inflight int
	// park is the overview anchor (an index) a steer or a band step owes
	// once its file's lines are in; -1 = none.
	park int
	// want is a (file, 1-based line) owed once that file loads — the place
	// S keeps when single turns stacked.
	want struct {
		file, line int
		set        bool
	}
}

// stackPaneMsg is one file's load for the stream of the document tagged tag.
type stackPaneMsg struct {
	slotStamp
	tag   string
	gen   int64
	file  int
	lines []contentLine
	err   error
}

// stackMaxLoads caps the file reads a stack has out at once.
const stackMaxLoads = 3

// splitStackGen numbers the stack panes of the process: each one's load
// generation (a package counter — the Model is a value, a field bump made
// while building a pane would be lost with the copy).
var splitStackGen atomic.Int64

// newStackPane builds the pane over the stack's files (stackFiles); over
// streamFoldOver files all but the overview's selected anchor's file open
// folded. Nothing is loaded yet: the caller's steer (or pump) starts the
// loads.
func (m Model) newStackPane(sv *splitView) pane {
	ov := sv.left.doc()
	target := ""
	if ov != nil && ov.ov != nil && ov.ov.sel >= 0 && ov.ov.sel < len(ov.ov.anchors) {
		target, _, _, _ = m.anchorPlace(ov, ov.ov.sel)
	}
	files, key, title := m.stackFiles(ov)
	s := &fileStream{files: files}
	if len(s.files) > streamFoldOver {
		for i := range s.files {
			s.files[i].folded = s.files[i].path != target
			s.files[i].autoFold = s.files[i].folded
		}
	}
	d := m.newOpenFile(fileSource{kind: srcReviewStack, rev: key}, "files-"+key)
	d.title = title
	d.stream, d.from = s, ov
	p := &stackPane{docPane: docPane{d: d, stackable: true}, sv: sv, stream: s, gen: splitStackGen.Add(1), park: -1}
	p.rebuild(m)
	return p
}

// stackFiles is the stack's file list for overview ov (spec
// 2026-10-10-overview-split-all §5): its review tree's files when that
// review view is open, else the files ov's anchors name — a file anchor's
// path, a note anchor's file — each once, first-anchored order; with the
// key the stack document is filed under and its title.
func (m Model) stackFiles(ov *openFile) (files []streamFile, key, title string) {
	if st := m.reviewOf(ov); st != nil && m.filesView != nil {
		for _, l := range m.filesView.lines {
			if l.path == "" {
				continue
			}
			f := streamFile{path: l.path, status: l.status, notes: st.counts[l.path]}
			if l.status == "D" { // the review deleted it: nothing to read at the tip
				f.loaded, f.err = true, errors.New(i18n.T("(not at this commit)"))
			}
			files = append(files, f)
		}
		return files, st.id, i18n.T("Files: %s", reviewLabel(st.review))
	}
	seen := map[string]bool{}
	for i := range ov.ov.anchors {
		path, _, _, ok := m.anchorPlace(ov, i)
		if !ok || seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, streamFile{path: path})
	}
	return files, ov.id(), i18n.T("Files: %s", ov.title)
}

// rebuild lays the stream out again keeping the cursor's (file, line) and
// its screen row — a file loading above the reader must not move them. A view
// scrolled away from the cursor (a page, the wheel) keeps its top line
// instead: a file unfolding or arriving there must not pull it back.
func (p *stackPane) rebuild(m Model) {
	d, s := p.d, p.stream
	rows, _ := p.sv.geomOf(m, p)
	// where is a stream line, as (file, offset into its body); back maps it
	// into the new stream, ok false when that file shows no lines any more.
	type place struct{ file, off int }
	where := func(line int) place {
		f := s.fileAt(line)
		if f < 0 {
			return place{-1, 0}
		}
		return place{f, line - s.files[f].body}
	}
	back := func(pl place) (int, bool) {
		if pl.file < 0 || pl.file >= len(s.files) || pl.off < 0 || !s.files[pl.file].shows() || pl.off >= len(s.files[pl.file].lines) {
			return 0, false
		}
		return s.files[pl.file].body + pl.off, true
	}
	file, off := -1, 0
	var selA, selE place
	if len(d.p.lines) > 0 {
		cur := where(d.p.cur)
		file, off = cur.file, cur.off
		if d.p.lsel.on {
			selA, selE = where(d.p.lsel.anchor), where(d.p.lsel.end)
		}
	}
	var csA, csC place // the character selection's ends, when its mode is on
	csOn := d.p.cs.on && !d.p.previewCharStale()
	if csOn {
		csA, csC = where(d.p.cs.anchor.row), where(d.p.cs.cur.row)
	}
	screenRow := d.p.cur - d.p.sel
	top := where(d.p.sel)
	sel := d.p.lsel
	d.p.lines = s.build()
	p.mapBoxes() // the files' notes follow their lines to the new stream places
	d.p.lsel.clear()
	if sel.on { // a marked range stays on its file's lines (a load elsewhere renumbers the stream)
		a, okA := back(selA)
		e, okE := back(selE)
		if okA && (okE || !sel.fixed) {
			sel.anchor, sel.end = a, e
			d.p.lsel = sel
		}
	}
	if csOn { // the mode stays on its file's lines (the line selection's rule)
		a, okA := back(csA)
		c, okC := back(csC)
		if okC && !okA && !d.p.cs.fixed {
			a, okA, csA = c, true, csC
		}
		if okA && okC && csA.file == csC.file {
			d.p.cs.anchor.row, d.p.cs.cur.row = a, c
			d.p.csRows = previewCharRows(d.p, s, csC.file)
			d.p.csLines = d.p.lines
		} else {
			d.p.previewCharLeave() // its file shows no lines any more
		}
	}
	if d.p.search.active() { // the hits index the old stream
		d.p.search.refindFrom(previewSearchLines(d.p), d.p.searchPos(rows))
	}
	if file < 0 {
		return
	}
	if off < 0 { // on the header (or the blank/rule above the body): stay on the header
		d.p.cur = s.files[file].hdr
	} else {
		d.p.cur = s.streamLine(file, off+1)
	}
	if screenRow >= 0 && screenRow < rows {
		d.p.sel = d.p.clampTop(d.p.cur-screenRow, rows)
		d.p.ensureCursorVisible(rows)
		return
	}
	if top.file >= 0 {
		d.p.sel = d.p.clampTop(s.lineOf(top.file, top.off), rows)
	}
}

// mapBoxes lays every shown file's notes onto the stream: file line k is
// stream line body+k (1-based), as the bands are mapped.
func (p *stackPane) mapBoxes() {
	var out []agentdocs.Note
	for _, f := range p.stream.files {
		if !f.shows() {
			continue
		}
		for _, b := range f.boxes {
			if b.End > len(f.lines) {
				continue
			}
			b.Start += f.body
			b.End += f.body
			out = append(out, b)
		}
	}
	p.d.reviewNotes = out
	p.d.syncNoteRows()
}

// splitStackNotesMsg is a review's remarks on stream file `file` of the stack
// tagged tag (generation gen).
type splitStackNotesMsg struct {
	slotStamp
	tag   string
	gen   int64
	file  int
	notes []domain.ResolvedNote
	err   error
}

// fileNotes gives file i, just loaded, its notes: a review's remarks are
// fetched off the UI thread (the command); an agent overview's notes come
// from the store at once, only while they sit on exactly these lines.
func (m Model) fileNotes(p *stackPane, i int) tea.Cmd {
	ov, f := p.sv.left.doc(), &p.stream.files[i]
	if ov == nil || !f.shows() {
		return nil
	}
	if ov.src.kind.storedOverview() {
		if m.svc == nil {
			return nil
		}
		svc, kind, id, path, lines, slot, tag, gen := m.svc, ov.src.kind, ov.src.rev, f.path, rawOf(f.lines), m.stamp(), p.d.tag, p.gen
		return func() tea.Msg {
			ns, err := storedFileNotes(svc, kind, id, path, lines)
			return splitStackNotesMsg{slotStamp: slot, tag: tag, gen: gen, file: i, notes: ns, err: err}
		}
	}
	if m.docs != nil && anchorSource(ov).kind == srcWorktree {
		if ns, fp := m.docs.Notes(domain.CheckoutKey(m.currentWorktree), f.path); len(ns) > 0 && fp == agentdocs.Print(rawOf(f.lines)) {
			f.boxes, f.notes = ns, len(ns)
		}
	}
	return nil
}

// stackNotesLanded adopts a file's remarks for the stack still showing it.
func (m Model) stackNotesLanded(msg splitStackNotesMsg) Model {
	_, p := m.stackPaneByTag(msg.tag)
	if p == nil || p.gen != msg.gen || msg.err != nil || msg.file < 0 || msg.file >= len(p.stream.files) {
		return m
	}
	var owner fileSourceKind
	if ov := p.sv.left.doc(); ov != nil {
		owner = ov.src.kind
	}
	rows, _ := p.sv.geomOf(m, p)
	pp := p.d.p
	shown := pp.cur >= pp.sel && pp.cur <= pp.lastVisible(rows)
	p.stream.files[msg.file].boxes = remarkBoxes(msg.notes, owner)
	p.mapBoxes()
	if shown { // a box above the cursor must not push it out…
		pp.ensureCursorVisible(rows)
	} // …and a view scrolled away from it stays where the reader put it
	return m
}

func (p *stackPane) title() string {
	k := max(p.stream.fileAt(p.d.p.cur)+1, 1)
	return p.d.title + "  " + i18n.T("file %d/%d", k, len(p.stream.files))
}

// render: the embedded docPane's render would call ITS title (Go has no
// virtual dispatch through an embedded struct), so the stack draws itself.
func (p *stackPane) render(m Model, w, h int, focused bool) string {
	return m.renderPreviewBoxOpts(p.d.p, p.title(), w, h, previewBoxOpts{focused: focused, cursorOff: !focused, single: true})
}

func (p *stackPane) hint(Model) string {
	if h, on := paneCharHint(p.d); on {
		return h
	}
	return i18n.T("[n/p] anchor  [N/P] file  [-] fold  [_] fold all  [S] single  [alt+↑↓] cursor  [spc] mark  [/] find  [:] line")
}

// loadCmd reads file i of the stream off the UI thread, at the reviewed tip
// (the working tree for a working review).
func (p *stackPane) loadCmd(m Model, i int) tea.Cmd {
	path := p.stream.files[i].path
	ov := p.sv.left.doc()
	if ov == nil || ov.ov == nil {
		return nil // no overview to say which commit: the split is closing
	}
	src, svc := anchorSource(ov), m.svc
	loader := func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, src.rev, path) }
	if src.kind == srcWorktree {
		loader = func(ctx context.Context) ([]byte, error) { return svc.WorktreeFile(ctx, path) }
	}
	read := loadFileContentSrcCmd(p.d.tag, path, m.cfg.UI.SyntaxOn(), loader)
	slot, tag, gen := m.stamp(), p.d.tag, p.gen
	return func() tea.Msg {
		r := read().(fileContentMsg)
		return stackPaneMsg{slotStamp: slot, tag: tag, gen: gen, file: i, lines: r.lines, err: r.err}
	}
}

// pump unfolds the files on screen the stream folded itself, then starts
// loads for unfolded files not yet read, the cursor's file first, at most
// stackMaxLoads at once.
func (p *stackPane) pump(m Model) tea.Cmd {
	p.unfoldInView(m)
	var cmds []tea.Cmd
	n := len(p.stream.files)
	at := max(p.stream.fileAt(p.d.p.cur), 0)
	for k := 0; k < n && p.inflight < stackMaxLoads; k++ {
		i := (at + k) % n
		f := &p.stream.files[i]
		if f.folded || f.loaded || f.loading {
			continue
		}
		f.loading = true
		p.inflight++
		cmds = append(cmds, p.loadCmd(m, i))
	}
	return tea.Batch(cmds...)
}

// stackPaneByTag finds the split and stack pane whose document is tagged tag.
func (m Model) stackPaneByTag(tag string) (*splitView, *stackPane) {
	if m.layers == nil {
		return nil, nil
	}
	for _, l := range m.layers.entries {
		if sv, ok := l.(*splitView); ok {
			if p, ok := sv.right.(*stackPane); ok && p.d.tag == tag {
				return sv, p
			}
		}
	}
	return nil, nil
}

// stackPaneLanded is a load's arrival: the lines go into their file, the
// stream is rebuilt around the reader, a parked place lands, the next loads
// start. An answer for a pane that is gone, or an older one, is dropped.
func (m Model) stackPaneLanded(msg stackPaneMsg) (Model, tea.Cmd) {
	sv, p := m.stackPaneByTag(msg.tag)
	if p == nil || p.gen != msg.gen || msg.file < 0 || msg.file >= len(p.stream.files) {
		return m, nil
	}
	f := &p.stream.files[msg.file]
	if f.loading {
		p.inflight = max(p.inflight-1, 0)
	}
	f.loading, f.loaded, f.lines, f.err = false, true, msg.lines, nil
	switch {
	case msg.err != nil:
		f.lines, f.err = nil, errors.New(i18n.T("(load failed: %s)", msg.err.Error()))
	case len(msg.lines) > 0 && !msg.lines[0].src: // a placeholder the loader named (binary, too large, an image)
		f.lines, f.err = nil, errors.New(msg.lines[0].text)
	}
	if ov := sv.left.doc(); ov != nil && ov.ov != nil { // the steer's rule: a read that found no file
		m.markFileAnchors(ov, f.path, readGone(msg.err, anchorSource(ov).kind == srcWorktree))
	}
	notes := m.fileNotes(p, msg.file)
	p.rebuild(m)
	rows, _ := sv.geomOf(m, p)
	if p.want.set && p.want.file == msg.file {
		p.want.set = false
		p.d.p.cur = p.stream.streamLine(msg.file, p.want.line)
		p.d.p.sel = p.d.p.clampTop(p.d.p.cur-rows/2, rows)
	}
	if ov := sv.left.doc(); p.park >= 0 && ov != nil && p.park < len(ov.ov.anchors) && m.parkedIn(ov, p.park, f.path) {
		i := p.park
		p.park = -1
		m = m.stackLand(sv, p, i)
	}
	return m, tea.Batch(p.pump(m), notes)
}

// parkedIn reports whether overview anchor i opens in the file at path.
func (m Model) parkedIn(ov *openFile, i int, path string) bool {
	at, _, _, ok := m.anchorPlace(ov, i)
	return ok && at == path
}

// fileIndex is path's index in the stream, or -1.
func (p *stackPane) fileIndex(path string) int {
	for i, f := range p.stream.files {
		if f.path == path {
			return i
		}
	}
	return -1
}

// stackLand puts the cursor on overview anchor i's line in the stream,
// centred, and makes it the current band. The overview's selection is not
// touched: a steer has selected i already, and n / p only mark it (R2).
func (m Model) stackLand(sv *splitView, p *stackPane, i int) Model {
	path, line, dest, ok := m.anchorPlace(sv.left.doc(), i)
	fi := p.fileIndex(path)
	if !ok || fi < 0 {
		return m
	}
	rows, _ := sv.geomOf(m, p)
	p.d.anchorCur = dest
	p.d.p.cur = p.stream.streamLine(fi, line)
	p.d.p.sel = p.d.p.clampTop(p.d.p.cur-rows/2, rows)
	return m
}

// stackSteer shows overview anchor i in the stream: its file unfolded and,
// once loaded, the cursor on its line.
func (m Model) stackSteer(sv *splitView, p *stackPane, i int) (Model, tea.Cmd) {
	path, _, _, ok := m.anchorPlace(sv.left.doc(), i)
	fi := p.fileIndex(path)
	if !ok {
		return m, nil
	}
	if fi < 0 {
		m.statusMsg = i18n.T("no file %s", path)
		return m, nil
	}
	p.park, p.want.set = -1, false // the newest step wins
	f := &p.stream.files[fi]
	if f.folded {
		f.folded = false
		p.rebuild(m)
	}
	if f.loaded {
		return m.stackLand(sv, p, i), nil
	}
	p.park = i
	rows, _ := sv.geomOf(m, p)
	p.d.p.cur = f.hdr
	p.d.p.ensureCursorVisible(rows)
	return m, p.pump(m)
}

// stepStreamAnchor is n/p in the stream: the next/previous overview anchor
// in stream order (file order, then line) across every file, wrapping; an
// anchor in a folded or unread file opens it and parks the landing.
func (m Model) stepStreamAnchor(sv *splitView, p *stackPane, dir int) (Model, tea.Cmd) {
	ov := sv.left.doc()
	if ov == nil || ov.ov == nil {
		return m, nil
	}
	type at struct{ i, file, line int }
	var order []at
	for fi, f := range p.stream.files {
		for i, a := range ov.ov.anchors {
			if a.plain || a.target.Note != "" || a.target.Path != f.path || a.target.Start <= 0 {
				continue
			}
			order = append(order, at{i, fi, a.target.Start})
		}
	}
	if len(order) == 0 {
		return m, nil
	}
	sort.SliceStable(order, func(a, b int) bool {
		if order[a].file != order[b].file {
			return order[a].file < order[b].file
		}
		return order[a].line < order[b].line
	})
	cur := -1
	for k, o := range order {
		if ov.ov.anchors[o.i].dest == p.d.anchorCur {
			cur = k
			break
		}
	}
	n := len(order)
	next := ((cur+dir)%n + n) % n
	if cur < 0 && dir < 0 {
		next = n - 1
	}
	wrapped := cur >= 0 && ((dir > 0 && next <= cur) || (dir < 0 && next >= cur))
	o := order[next]
	m, cmd := m.stackSteer(sv, p, o.i)
	label := ov.ov.anchors[o.i].target.Label
	if label == "" {
		label = ov.ov.anchors[o.i].dest
	}
	if wrapped {
		m.statusMsg = i18n.T("anchor %d/%d · %s · wrapped", next+1, n, label)
	} else {
		m.statusMsg = i18n.T("anchor %d/%d · %s", next+1, n, label)
	}
	return m, cmd
}

// update is a key on the pane; a file the stream folded itself unfolds
// once the key brings its header on screen (pump).
func (p *stackPane) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	nm, cmd := p.key(m, msg)
	if nm.splitLayer() != p.sv || p.sv.right != p {
		return nm, cmd // S swapped the pane, or the split closed
	}
	return nm, tea.Batch(cmd, p.pump(nm))
}

func (p *stackPane) key(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	d := p.d
	if s := msg.String(); s != "n" && s != "p" {
		// A parked landing belongs to the step that parked it: any other key
		// abandons it, so a late load never yanks the cursor from where the
		// reader went (the stacked diff's hunt rule).
		p.park, p.want.set = -1, false
	}
	if nm, cmd, ok := m.stackJumpKey(p, msg); ok { // a go-to-line being typed owns every key
		return nm, cmd
	}
	if nm, cmd, ok := m.previewCharKey(msg); ok { // modal: n/p, -, S and the rest wait
		return nm, cmd
	}
	if nm, cmd, ok := m.previewSelectKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.previewSearchKey(msg); ok {
		return nm, cmd
	}
	rows, _ := p.sv.geomOf(m, p)
	switch msg.String() {
	case "n":
		return m.stepStreamAnchor(p.sv, p, 1)
	case "p":
		return m.stepStreamAnchor(p.sv, p, -1)
	case "-":
		fi := p.stream.fileAt(d.p.cur)
		if fi < 0 {
			return m, nil
		}
		p.stream.files[fi].folded = !p.stream.files[fi].folded
		p.stream.files[fi].autoFold = false // the reader's own fold: it stays
		d.p.cur = p.stream.files[fi].hdr    // the cursor rests on the header it folded
		p.rebuild(m)
		d.p.cur = p.stream.files[fi].hdr
		d.p.ensureCursorVisible(rows)
		return m, p.pump(m)
	case "_":
		all := true
		for _, f := range p.stream.files {
			all = all && f.folded
		}
		for i := range p.stream.files {
			p.stream.files[i].folded, p.stream.files[i].autoFold = !all, false
		}
		p.rebuild(m)
		return m, p.pump(m)
	case "N", "P":
		dir := 1
		if msg.String() == "P" {
			dir = -1
		}
		if h := p.stream.nextHeader(d.p.cur, dir); h >= 0 {
			d.p.cur = h
			d.p.ensureCursorVisible(rows)
		}
		return m, p.pump(m)
	case "S":
		return m.toggleReviewStacked(p.sv)
	case "X", "ctrl+]", "y":
		return m, nil // the stream is no open file: nothing to close, background or copy whole
	}
	return p.docPane.update(m, msg)
}

// toggleReviewStacked is S on either pane: single ⇄ stacked, keeping the
// cursor's file and line.
func (m Model) toggleReviewStacked(sv *splitView) (Model, tea.Cmd) {
	ov := sv.left.doc()
	if ov == nil || ov.ov == nil {
		return m, nil
	}
	switch r := sv.right.(type) {
	case *stackPane:
		m = m.setReviewStackedPref(false)
		dp := &docPane{stackable: true}
		sv.right = dp
		fi := r.stream.fileAt(r.d.p.cur)
		if fi < 0 {
			return m, nil
		}
		f := r.stream.files[fi]
		line := 0
		if f.shows() && r.d.p.cur >= f.body {
			line = r.d.p.cur - f.body + 1
		}
		dest := ""
		for _, a := range ov.ov.anchors { // the current band carries over when it is in this file
			if a.dest == r.d.anchorCur && a.target.Path == f.path {
				dest = a.dest
			}
		}
		return m.docSteer(sv, dp, ov, anchor{dest: dest, target: anchorTarget(f.path, line)})
	case *docPane:
		m = m.setReviewStackedPref(true)
		path, line, cur := "", 0, ""
		if r.d != nil {
			path, line, cur = r.d.path, r.d.p.cur+1, r.d.anchorCur
			r.d.backgrounded = true
		}
		p := m.newStackPane(sv).(*stackPane)
		sv.right = p
		p.d.anchorCur = cur
		if fi := p.fileIndex(path); fi >= 0 {
			p.stream.files[fi].folded = false
			p.rebuild(m)
			p.d.p.cur = p.stream.files[fi].hdr
			p.want.file, p.want.line, p.want.set = fi, line, true
		}
		return m, p.pump(m)
	}
	return m, nil
}

// visibleHeaders are the files whose header row is on the pane's screen.
func (p *stackPane) visibleHeaders(m Model) map[int]bool {
	rows, inner := p.sv.textGeomOf(m, p)
	w := max(inner-p.d.gutterW(), 1)
	out := map[int]bool{}
	for r := range rows {
		li := p.d.p.lineAtRow(r, rows, w)
		if li < 0 {
			continue
		}
		if fi := p.stream.fileAt(li); fi >= 0 && p.stream.files[fi].hdr == li {
			out[fi] = true
		}
	}
	return out
}

// scrolled runs move (a wheel scroll) and loads what it brought into view.
func (p *stackPane) scrolled(m Model, move func()) tea.Cmd {
	move()
	return p.pump(m)
}

// unfoldInView opens every file the stream folded on its own whose header is
// on screen — at open, after a page, a scroll or a step: a file shown is a
// file being read, so a big stream loads what is read like a small one. A
// file folded by hand stays folded. Unfolding shifts the layout, so it
// repeats until the screen holds no such header (each round opens one or
// more files).
func (p *stackPane) unfoldInView(m Model) {
	for range p.stream.files {
		changed := false
		for i := range p.visibleHeaders(m) {
			if f := &p.stream.files[i]; f.folded && f.autoFold {
				f.folded, f.autoFold = false, false
				changed = true
			}
		}
		if !changed {
			return
		}
		p.rebuild(m)
	}
}
