package tui

import (
	"context"
	"errors"
	"sort"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

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

// newStackPane builds the pane over the review tree's files (its rows with
// a path, in tree order); over streamFoldOver files all but the overview's
// selected anchor's file open folded. Nothing is loaded yet: the caller's
// steer (or pump) starts the loads.
func (m Model) newStackPane(sv *splitView, st *reviewViewState) pane {
	ov := sv.left.doc()
	target := ""
	if ov != nil && ov.ov != nil && ov.ov.sel >= 0 && ov.ov.sel < len(ov.ov.anchors) {
		target = ov.ov.anchors[ov.ov.sel].target.Path
	}
	s := &fileStream{}
	if m.filesView != nil {
		for _, l := range m.filesView.lines {
			if l.path == "" {
				continue
			}
			f := streamFile{path: l.path, status: l.status, notes: st.counts[l.path]}
			if l.status == "D" { // the review deleted it: nothing to read at the tip
				f.loaded, f.err = true, errors.New(i18n.T("(not at this commit)"))
			}
			s.files = append(s.files, f)
		}
	}
	if len(s.files) > streamFoldOver {
		for i := range s.files {
			s.files[i].folded = s.files[i].path != target
		}
	}
	d := m.newOpenFile(fileSource{kind: srcReviewStack, rev: st.id}, "files-"+st.id)
	d.title = i18n.T("Files: %s", reviewLabel(st.review))
	d.stream, d.from = s, ov
	p := &stackPane{docPane: docPane{d: d, stackable: true}, sv: sv, stream: s, gen: splitStackGen.Add(1), park: -1}
	p.rebuild(m)
	return p
}

// rebuild lays the stream out again keeping the cursor's (file, line) and
// its screen row — a file loading above the reader must not move them.
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
	screenRow := d.p.cur - d.p.sel
	sel := d.p.lsel
	d.p.lines = s.build()
	d.p.lsel.clear()
	if sel.on { // a marked range stays on its file's lines (a load elsewhere renumbers the stream)
		a, okA := back(selA)
		e, okE := back(selE)
		if okA && (okE || !sel.fixed) {
			sel.anchor, sel.end = a, e
			d.p.lsel = sel
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
	}
	d.p.ensureCursorVisible(rows)
}

func (p *stackPane) title() string {
	k := max(p.stream.fileAt(p.d.p.cur)+1, 1)
	return p.d.title + "  " + i18n.T("file %d/%d", k, len(p.stream.files))
}

// render: the embedded docPane's render would call ITS title (Go has no
// virtual dispatch through an embedded struct), so the stack draws itself.
func (p *stackPane) render(m Model, w, h int, focused bool) string {
	return m.renderPreviewBoxOpts(p.d.p, p.title(), w, h, previewBoxOpts{focused: focused, cursorOff: !focused})
}

func (p *stackPane) hint(Model) string {
	return i18n.T("[n/p] anchor  [N/P] file  [-] fold  [_] fold all  [S] single  [alt+↑↓] line  [spc] mark  [/] find")
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

// pump starts loads for unfolded files not yet read, the cursor's file
// first, at most stackMaxLoads at once.
func (p *stackPane) pump(m Model) tea.Cmd {
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
	p.rebuild(m)
	rows, _ := sv.geomOf(m, p)
	if p.want.set && p.want.file == msg.file {
		p.want.set = false
		p.d.p.cur = p.stream.streamLine(msg.file, p.want.line)
		p.d.p.sel = p.d.p.clampTop(p.d.p.cur-rows/2, rows)
	}
	if ov := sv.left.doc(); p.park >= 0 && ov != nil && p.park < len(ov.ov.anchors) && ov.ov.anchors[p.park].target.Path == f.path {
		i := p.park
		p.park = -1
		m = m.stackLand(sv, p, i)
	}
	return m, p.pump(m)
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
	ov := sv.left.doc()
	a := ov.ov.anchors[i]
	fi := p.fileIndex(a.target.Path)
	if fi < 0 {
		return m
	}
	rows, _ := sv.geomOf(m, p)
	p.d.anchorCur = a.dest
	p.d.p.cur = p.stream.streamLine(fi, a.target.Start)
	p.d.p.sel = p.d.p.clampTop(p.d.p.cur-rows/2, rows)
	return m
}

// stackSteer shows overview anchor i in the stream: its file unfolded and,
// once loaded, the cursor on its line.
func (m Model) stackSteer(sv *splitView, p *stackPane, i int) (Model, tea.Cmd) {
	a := sv.left.doc().ov.anchors[i]
	fi := p.fileIndex(a.target.Path)
	if fi < 0 {
		m.statusMsg = i18n.T("no file %s", a.target.Path)
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

func (p *stackPane) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	d := p.d
	if s := msg.String(); s != "n" && s != "p" {
		// A parked landing belongs to the step that parked it: any other key
		// abandons it, so a late load never yanks the cursor from where the
		// reader went (the stacked diff's hunt rule).
		p.park, p.want.set = -1, false
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
		d.p.cur = p.stream.files[fi].hdr // the cursor rests on the header it folded
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
			p.stream.files[i].folded = !all
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
	if m.filesReview == nil { // the review the stack lists is gone
		m.statusMsg = i18n.T("the review is no longer open")
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
		p := m.newStackPane(sv, m.filesReview).(*stackPane)
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
