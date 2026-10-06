package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// historyPage is one walk's worth of file history; load more asks for the
// next page.
const historyPage = 200

// A walk hands commits to the UI in batches: at historyBatchMax pending
// commits, or historyBatchDelay after the first pending one — so the newest
// commit shows within 50 ms even when git takes seconds to find the next.
const (
	historyBatchMax   = 20
	historyBatchDelay = 50 * time.Millisecond
)

// historyListMaxW caps the commit-list column so a wide terminal gives the
// extra width to the diff pane, not the list.
const historyListMaxW = 60

// navContext identifies the file + revision a history/blame view explores.
// rev "" means the working-tree context (history from HEAD).
type navContext struct {
	path string
	rev  string
}

// historyView is the file-history surface: commits left, the file's diff at the
// selected commit on the right (reusing the diff pane). The list streams in
// (loadHistoryListCmd): commits appear as git finds them.
type historyView struct {
	ctx     navContext
	commits []model.FileCommit
	sel     int
	mode    dispMode  // left list text display mode; z cycles
	hscroll int       // modeScroll horizontal offset
	loading bool      // no commit has arrived yet
	err     error     // the walk failed before finding any commit
	diff    *diffView // right pane (reuses diffView rendering + guards)
	diffTag string    // gates stale right-pane loads

	// The streamed walk (loadHistoryListCmd). gen gates a superseded walk's
	// chunks; cancel stops the running git; start pins the first walk's start
	// sha so a load-more walk skips exactly the commits already shown.
	cancel    context.CancelFunc
	gen       int
	streaming bool  // a walk is running
	streamErr error // the walk failed after some commits arrived
	start     string
	more      bool // the last walk filled its page: older commits may exist
	advance   bool // load more: step onto the first new commit when it lands
}

func newHistoryView(ctx navContext) *historyView {
	return &historyView{ctx: ctx, loading: true}
}

// stop cancels a running walk (its git process is terminated).
func (h *historyView) stop() {
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
}

// historyChunkMsg is one batch of a streamed walk. next yields the walk's
// following message; the handler re-arms it until done.
type historyChunkMsg struct {
	view    *historyView
	gen     int
	start   string // the walk's resolved start rev (pinned for load more)
	commits []model.FileCommit
	done    bool
	more    bool // done and the walk hit its limit
	err     error
	next    <-chan historyChunkMsg
}

type historyDiffMsg struct {
	tag  string
	view *diffView
}

// loadHistoryListCmd starts a streamed walk for h: the first walk, or — with
// commits already listed — load more, which re-walks from the pinned start
// with a page more and drops the commits already shown (git log --follow
// ignores --skip).
func (m Model) loadHistoryListCmd(h *historyView) tea.Cmd {
	h.stop()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.gen++
	h.streaming = true
	h.streamErr = nil
	w := historyWalk{
		svc:   m.svc,
		gen:   h.gen,
		view:  h,
		rev:   h.ctx.rev,
		pin:   h.start == "",
		path:  h.ctx.path,
		skip:  len(h.commits),
		limit: len(h.commits) + historyPage,
	}
	if !w.pin {
		w.rev = h.start
	}
	// The walk starts when the cmd runs, not when it is built: a caller that
	// builds the cmd and drops it starts no git.
	return func() tea.Msg {
		out := make(chan historyChunkMsg)
		go w.run(ctx, out)
		return <-out
	}
}

// waitHistoryChunk delivers a walk's next message.
func waitHistoryChunk(ch <-chan historyChunkMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// historyWalk is one streamed git log --follow, run off the UI thread.
type historyWalk struct {
	svc         *domain.Service
	view        *historyView
	gen         int
	rev, path   string
	pin         bool
	skip, limit int
}

// run streams the walk into out in batches. Every send selects on ctx, so a
// walk whose view went away never blocks forever.
func (w historyWalk) run(ctx context.Context, out chan historyChunkMsg) {
	start := w.rev
	if w.pin {
		r := w.rev
		if r == "" {
			r = "HEAD"
		}
		if sha, err := w.svc.RevParse(ctx, r); err == nil {
			start = sha
		}
	}
	found := make(chan model.FileCommit, historyBatchMax)
	type result struct {
		n   int
		err error
	}
	finished := make(chan result, 1)
	go func() {
		n := 0
		err := w.svc.FileLogStream(ctx, start, w.path, w.limit, func(fc model.FileCommit) {
			n++
			if n <= w.skip {
				return
			}
			select {
			case found <- fc:
			case <-ctx.Done():
			}
		})
		finished <- result{n: n, err: err}
	}()

	msg := historyChunkMsg{view: w.view, gen: w.gen, start: start, next: out}
	var batch []model.FileCommit
	var tick <-chan time.Time
	send := func(done, more bool, err error) bool {
		m := msg
		m.commits, m.done, m.more, m.err = batch, done, more, err
		select {
		case out <- m:
			msg.start = "" // only the first message carries it
			batch, tick = nil, nil
			return true
		case <-ctx.Done():
			return false
		}
	}
	for {
		select {
		case fc := <-found:
			batch = append(batch, fc)
			if len(batch) == 1 {
				tick = time.After(historyBatchDelay)
			}
			if len(batch) >= historyBatchMax && !send(false, false, nil) {
				return
			}
		case <-tick:
			if !send(false, false, nil) {
				return
			}
		case r := <-finished:
			// Every emit's send completed before finished was written: drain.
			for drained := false; !drained; {
				select {
				case fc := <-found:
					batch = append(batch, fc)
				default:
					drained = true
				}
			}
			send(true, r.err == nil && r.n >= w.limit, r.err)
			return
		case <-ctx.Done():
			return
		}
	}
}

// historyLive reports whether h is still a view the user can come back to:
// on the stack (a console's parked views are put back for non-key messages,
// see dispatchParkedAware) or parked by a hand-off to the files view.
func (m Model) historyLive(h *historyView) bool {
	if m.hasLayer(h) {
		return true
	}
	for _, l := range m.filesReturnLayers {
		if l == h {
			return true
		}
	}
	return false
}

// onHistoryChunk applies one batch of a streamed walk.
func (m Model) onHistoryChunk(msg historyChunkMsg) (Model, tea.Cmd) {
	h := msg.view
	if h == nil || h.gen != msg.gen {
		return m, nil // superseded: loadHistoryListCmd cancelled that walk
	}
	if !m.historyLive(h) {
		h.stop()
		return m, nil
	}
	if msg.start != "" {
		h.start = msg.start
	}
	var cmds []tea.Cmd
	if len(msg.commits) > 0 {
		first := len(h.commits) == 0
		h.commits = append(h.commits, msg.commits...)
		h.loading = false
		switch {
		case first:
			h.sel = 0
			cmds = append(cmds, h.selectCmd(m))
		case h.advance:
			h.advance = false
			if h.sel < len(h.commits)-1 {
				h.sel++
				cmds = append(cmds, h.selectCmd(m))
			}
		}
	}
	if !msg.done {
		return m, tea.Batch(append(cmds, waitHistoryChunk(msg.next))...)
	}
	h.stop()
	h.streaming, h.loading, h.advance = false, false, false
	h.more = msg.more
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		if len(h.commits) == 0 {
			h.err = msg.err
		} else {
			h.streamErr = msg.err
		}
	}
	return m, tea.Batch(cmds...)
}

// headerSuffix is the walk's state after the path: the running count, or a
// failure that came after some commits were already listed.
func (h *historyView) headerSuffix() string {
	switch {
	case h.streaming && len(h.commits) > 0:
		return i18n.T(" · loading… %d found", len(h.commits))
	case h.streamErr != nil:
		return i18n.T(" · error: %s", h.streamErr.Error())
	}
	return ""
}

// historyDiffSources builds the cache key + byte sources for fc's change (the
// file at fc vs its first parent, addressing the correct possibly-renamed blob
// names). Shared by the right-pane and full-screen loaders so the A/D/rename
// handling can't drift between them.
func (m Model) historyDiffSources(fc model.FileCommit) (key string, oldSrc, newSrc domain.ByteSource) {
	svc := m.svc
	// Immutable: parent(hash)→hash for a path always yields the same bytes.
	key = fc.Hash + "^.." + fc.Hash + ":" + fc.Path
	if fc.Status != "A" {
		p := fc.Path
		if fc.OldPath != "" {
			p = fc.OldPath
		}
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, fc.Hash+"^", p) }
	}
	if fc.Status != "D" {
		newSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, fc.Hash, fc.Path) }
	}
	return key, oldSrc, newSrc
}

// historyNoteAddress is the note address a file-history diff shows: the same
// first-parent → commit pair loadCommitDiffCmd renders, so a note taken here
// and one taken on the commit's own file diff address the same two texts.
// Renames keep the address on fc.Path — the name the NEW side carries.
func historyNoteAddress(fc model.FileCommit) model.FileAddress {
	return model.FileAddress{State: model.StateCommitted, Commit: fc.Hash, Path: fc.Path}
}

// loadHistoryDiffCmd builds the right-pane diff for fc: the file at fc vs its
// first parent, addressing the correct (possibly renamed) blob names.
func (m Model) loadHistoryDiffCmd(fc model.FileCommit, tag string) tea.Cmd {
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	v := &diffView{title: fc.Path, context: "@ " + shortHash(fc.Hash) + " " + fc.Subject, rev: fc.Hash, partial: m.diffPartial,
		noteAddr: historyNoteAddress(fc)}
	key, oldSrc, newSrc := m.historyDiffSources(fc)
	return func() tea.Msg {
		out, err := differ.Diff(context.Background(), domain.Request{Key: key, Path: fc.Path, Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return historyDiffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return historyDiffMsg{tag: tag, view: v}
	}
}

// loadHistoryDiffFullCmd builds the SAME diff as loadHistoryDiffCmd but yields a
// diffMsg so it lands in the full-screen standalone diff layer (the diff key is shared, so
// the right-pane load already warmed the cache). Used by enter.
func (m Model) loadHistoryDiffFullCmd(fc model.FileCommit, tag string) tea.Cmd {
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	width, _ := m.overlayDims()
	v := &diffView{title: fc.Path, context: "@ " + shortHash(fc.Hash) + " " + fc.Subject, rev: fc.Hash, partial: m.diffPartial, long: m.diffLong, width: width,
		noteAddr: historyNoteAddress(fc)}
	key, oldSrc, newSrc := m.historyDiffSources(fc)
	return func() tea.Msg {
		out, err := differ.Diff(context.Background(), domain.Request{Key: key, Path: fc.Path, Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}

// selectCmd (re)loads the right pane for the current selection.
func (h *historyView) selectCmd(m Model) tea.Cmd {
	if h.sel < 0 || h.sel >= len(h.commits) {
		return nil
	}
	fc := h.commits[h.sel]
	h.diffTag = "histdiff:" + fc.Hash + ":" + h.ctx.path
	h.diff = &diffView{title: fc.Path, context: "@ " + shortHash(fc.Hash) + " " + fc.Subject, loading: true}
	return m.loadHistoryDiffCmd(fc, h.diffTag)
}

// Temporary stubs so the package compiles; real implementations land in the
// next task.
// historyBodyRows is the list/pane height: full height minus header + hint.
func (m Model) historyBodyRows() int {
	_, h := m.overlayDims()
	n := h - 2
	if n < 1 {
		n = 1
	}
	return n
}

// listRows lays each commit out as a two-line entry: a meta line
// (date  author  hash  status) then the subject, wrapped onto indented
// continuation lines so a long title stays fully readable. Returns the
// flattened rows plus the selected entry's meta-row index (the window anchor).
func (h *historyView) listRows(listW int) ([]winRow, int) {
	const indent = "    "
	var rows []winRow
	anchor := 0
	s := st()
	for i, fc := range h.commits {
		prefix := "  "
		var st lipgloss.Style
		if i == h.sel {
			prefix = "> "
			st = s.selectedRow
			anchor = len(rows)
		}
		date := commitDateString(fc.Commit)
		hash := shortHash(fc.Hash)
		// Truncate the author alone (not the composed line) so a long name can
		// never push the hash/status off the right edge.
		authorW := listW - lipgloss.Width(prefix+date+hash+fc.Status) - 6
		if authorW < 5 {
			authorW = 5
		}
		author := truncate(safeRowText(fc.Author), authorW)
		meta := truncate(prefix+date+"  "+author+"  "+hash+"  "+fc.Status, listW)
		rows = append(rows, winRow{text: meta, style: st})
		for _, seg := range wrapWidth(safeRowText(fc.Subject), listW-len(indent), 1<<20) {
			rows = append(rows, winRow{text: indent + seg, style: st})
		}
	}
	return rows, anchor
}

func (h *historyView) render(m Model, _ string) string {
	w, scrH := m.overlayDims()
	body := m.historyBodyRows()

	sfx := h.headerSuffix()
	pathW := w - lipgloss.Width(sfx)
	if pathW < 1 {
		pathW = 1
	}
	header := truncate(elidePath(i18n.T("history: %s", h.ctx.path), pathW)+sfx, w) // keep the file name
	hint := truncate(i18n.T("[↑↓] commit  [enter] diff  [e] editor  [esc] back"), w)

	// Left list. Right pane shown only when wide enough (>=60); else list-only.
	// The list is capped at historyListMaxW; extra width goes to the diff pane.
	split := w >= 60
	listW := w
	if split {
		listW = (w - 1) / 2
		if listW > historyListMaxW {
			listW = historyListMaxW
		}
	}

	wr, anchor := h.listRows(listW)
	win := renderWindow(wr, winOpts{w: listW, h: body, mode: h.mode, anchor: anchor, hscroll: h.hscroll})
	if h.loading {
		win = padLines(i18n.T("  (loading…)"), listW, body)
	} else if h.err != nil {
		win = padLines(i18n.T("  error: %s", h.err.Error()), listW, body)
	} else if len(h.commits) == 0 {
		win = padLines(i18n.T("  (no history)"), listW, body)
	}
	for len(win) < body {
		win = append(win, padRight("", listW))
	}

	var bodyStr string
	if split {
		right := h.renderRightPane(m, w-listW-1, body)
		rightCol := strings.Split(right, "\n")
		leftCol := make([]string, body)
		for i := 0; i < body; i++ {
			l := ""
			if i < len(win) {
				l = win[i]
			}
			r := ""
			if i < len(rightCol) {
				r = rightCol[i]
			}
			leftCol[i] = l + "│" + r
		}
		bodyStr = strings.Join(leftCol, "\n")
	} else {
		bodyStr = strings.Join(win, "\n")
	}

	out := header + "\n" + bodyStr + "\n" + hint
	return clipToHeight(out, scrH)
}

// renderRightPane draws the selected commit's diff using the shared diff pane.
func (h *historyView) renderRightPane(m Model, w, body int) string {
	if h.diff == nil {
		return padBox(i18n.T("  (select a commit)"), w, body)
	}
	v := h.diff
	switch {
	case v.loading:
		return padBox(i18n.T("  (loading…)"), w, body)
	case v.err != nil:
		return padBox(truncate(i18n.T("  error: %s", v.err.Error()), w), w, body)
	case v.binary && !v.hasImages():
		return padBox(i18n.T("  (binary file)"), w, body)
	case v.tooLarge:
		return padBox(i18n.T("  (file too large)"), w, body)
	}
	var lines []string
	if v.hasImages() {
		lines = v.imageLines(w, body)
	} else {
		lines = m.diffPaneLines(v, w, body, 0, 0, "off")
	}
	for len(lines) < body {
		lines = append(lines, "")
	}
	return strings.Join(lines[:body], "\n")
}

// padLines returns a body-high block whose first line is s (truncated to w),
// the rest blank — the list-pane placeholder for loading/error/empty states.
func padLines(s string, w, body int) []string {
	if body < 1 {
		body = 1
	}
	out := make([]string, body)
	out[0] = padRight(truncate(s, w), w)
	for i := 1; i < body; i++ {
		out[i] = padRight("", w)
	}
	return out
}

// padBox renders s as the first line of a w×body block, blank-filled.
func padBox(s string, w, body int) string {
	lines := make([]string, body)
	lines[0] = padRight(truncate(s, w), w)
	for i := 1; i < body; i++ {
		lines[i] = padRight("", w)
	}
	return strings.Join(lines, "\n")
}

func (h *historyView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case ".":
		return m.openActionMenu(), nil
	case "g": // global bookmark quick-switcher
		return m.openBookmarkSwitcher()
	case "G": // global shelf quick-switcher
		return m.openShelfSwitcher()
	case "F": // the working-tree files window (F)
		return m.openWorktreeFilesHere()
	case "ctrl+w":
		h.mode = h.mode.next()
		h.hscroll = 0
		return m, nil
	case "shift+left":
		if h.mode == modeScroll && h.hscroll > 0 {
			if h.hscroll -= m.hscrollStep(); h.hscroll < 0 {
				h.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if h.mode == modeScroll {
			h.hscroll += m.hscrollStep()
		}
		return m, nil
	// q is inert here: only the base layout quits on q. esc is the back key;
	// ctrl+c (handled above) remains the universal quit.
	case "esc", "h":
		h.stop()
		return m.popLayer(), nil
	case "b":
		if h.sel >= 0 && h.sel < len(h.commits) {
			fc := h.commits[h.sel]
			// Blame the file under the name it had *at this commit* (fc.Path):
			// across a rename/copy the current name need not exist in the
			// commit's tree, which would make `git blame` fail (exit 128).
			path := fc.Path
			if path == "" {
				path = h.ctx.path
			}
			ctx := navContext{path: path, rev: fc.Hash}
			bv := newBlameView(ctx)
			m = m.pushLayer(bv)
			return m, m.loadBlameCmd(ctx, bv.tag)
		}
	case "enter": // open the selected commit's diff full-screen
		if h.sel >= 0 && h.sel < len(h.commits) {
			fc := h.commits[h.sel]
			tag := "histdifffull:" + fc.Hash + ":" + h.ctx.path
			// openPickerDiff pushes the diff onto the stack above the history view.
			// The diff is the top layer and owns key routing; esc pops it and returns
			// to the history view beneath. From the diff, h reopens history.
			placeholder := &diffView{title: fc.Path, context: "@ " + shortHash(fc.Hash) + " " + fc.Subject, loading: true, partial: m.diffPartial, long: m.diffLong}
			return m.openPickerDiff(placeholder, tag, m.loadHistoryDiffFullCmd(fc, tag))
		}
	case "e": // open this commit's version of the file in $EDITOR (read-only)
		if r, ok := m.surfaceExternalRow(); ok {
			nm, cmd := r.run(m)
			return nm.(Model), cmd
		}
	case "down", "j":
		if h.sel < len(h.commits)-1 {
			h.sel++
			return m, h.selectCmd(m)
		}
	case "up", "k":
		if h.sel > 0 {
			h.sel--
			return m, h.selectCmd(m)
		}
	}
	return m, nil
}
