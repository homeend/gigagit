package tui

import (
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// steerStage says which load a parked navigate is waiting on. Each stage is
// drained by the SAME handler that already drains pendingGotoTip's cousin, so
// an async load is never raced.
type steerStage int

const (
	steerStageStatusRetry steerStage = iota // one status reload, then retry the lookup
	steerStageFiles                         // a commit's changed-file list
	steerStageDiff                          // the diff itself; land the cursor
	steerStagePreview                       // a merge preview's compare file list
)

// steerPendingTTL bounds a parked navigate. The user can close the view the
// command was waiting for, in which case its load never arrives; without a
// bound the pending would then land on an unrelated later diff.
const steerPendingTTL = 5 * time.Second

// pendingSteer is a navigate command parked until the load it needs arrives.
type pendingSteer struct {
	cmd    steer.Command
	stage  steerStage
	tag    string // steerStageDiff: the m.diffTag this landing belongs to
	hash   string // steerStageFiles: the commit whose file list is loading
	source string // steerStagePreview: the pair whose compare list is loading
	target string
	at     time.Time
}

// steerToPanels pops everything the refusal whitelist lets it pop, so the
// panel selection the pipeline is about to make is the one the user sees. The
// whitelist has already been enforced by steerRefusal: anything still on the
// stack here is a diff, history, blame view or plain content popup.
//
// It MOVES the user's view, so a pipeline calls it only once it has decided
// the command can be served — a refusal that first cleared the stack would be
// exactly the "throw away what the user is in the middle of" the whitelist
// exists to prevent.
func (m Model) steerToPanels() Model {
	if m.filesView != nil {
		m = m.closeFilesView() // also zeroes the right-column file preview
	} else if m.filesPreview != nil {
		m = m.closePreview() // defensive: a preview can only live inside a files view
	}
	if m.stashView != nil {
		m = m.closeStashView()
	}
	return m.clearLayers()
}

// steerFocus switches to a panel by its protocol name.
func (m Model) steerFocus(c steer.Command) (Model, tea.Cmd) {
	p, ok := panelFromProtoName(c.Panel)
	if !ok {
		return m, m.answerSteer(c, steerFail(c, "unknown panel "+strconv.Quote(c.Panel)))
	}
	// focus MOVES the view: steerToPanels below pops the very diff a parked
	// navigate is waiting on, whose diffMsg would then find no layer and return
	// early. Supersede it explicitly — the CLI waits two seconds, the pending's
	// TTL is five, so an expiry answer would reach nobody.
	var superseded tea.Cmd
	m, superseded = m.failPending("superseded by a later steering command")
	m = m.steerToPanels()
	if p == panelCommits {
		m = m.focusCommitsPanel()
	} else {
		m = m.activateTab(p)
	}
	m = m.steerNotice(i18n.T("▸ agent moved the focus"))
	return m, tea.Batch(superseded, m.answerSteer(c, steerOK(c, "focused "+c.Panel)))
}

// steerStep moves the open diff's cursor to the next/previous note. It is
// jumpNote, not the whole }/{ key arm: that arm's second behaviour (arm, then a
// SECOND press steps to the next noted FILE) is a two-keystroke interaction
// with no meaning for a one-shot command.
func (m Model) steerStep(c steer.Command) (Model, tea.Cmd) {
	dir := 0
	switch c.Step {
	case "next_note":
		dir = 1
	case "prev_note":
		dir = -1
	default:
		// Same rule as an unknown panel: a step gg does not have is named back
		// rather than silently rounded to "next".
		return m, m.answerSteer(c, steerFail(c, "unknown step "+strconv.Quote(c.Step)))
	}
	if m.diffLayer() == nil {
		return m, m.answerSteer(c, steerFail(c, "no diff is open"))
	}
	var moved bool
	m, moved = m.jumpNote(dir)
	if !moved {
		if dir > 0 {
			return m, m.answerSteer(c, steerFail(c, "no next note in this diff"))
		}
		return m, m.answerSteer(c, steerFail(c, "no previous note in this diff"))
	}
	what := "next"
	if dir < 0 {
		what = "previous"
	}
	return m, m.answerSteer(c, steerOK(c, "stepped to the "+what+" note"))
}

// steerNavigate is the whole navigate verb. It resolves what it can
// synchronously and parks the rest.
func (m Model) steerNavigate(c steer.Command) (Model, tea.Cmd) {
	switch {
	case c.Step != "":
		return m.steerStep(c)
	case c.Target != nil && c.Target.State == "preview":
		return m.steerNavigatePreview(c)
	case c.File != "" && c.Target != nil && c.Target.State == "commit":
		return m.steerNavigateCommitFile(c)
	case c.File != "":
		return m.steerNavigateStatusFile(c, false)
	case c.Commit != "":
		// Probe first: gotoLoadedCommit (and steerToPanels) MOVE the view, and
		// a commit the feed has not paged in is a refusal, not a reason to
		// close what the user was reading.
		if _, ok := m.steerCommitRow(c.Commit); !ok {
			return m, m.answerSteer(c, steerFail(c, "commit not loaded in the feed"))
		}
		nm := m.steerToPanels().steerClearCommitsFilter()
		nm, _, ok := nm.gotoLoadedCommit(c.Commit)
		if !ok {
			return m, m.answerSteer(c, steerFail(c, "commit not loaded in the feed"))
		}
		return nm, nm.answerSteer(c, steerOK(c, "revealed commit "+shortHash(c.Commit)))
	}
	return m, m.answerSteer(c, steerFail(c, "navigate needs a file, a commit or a step"))
}

// steerCommitRow is the Commits panel's equivalent of steerStatusRow: it scans
// the LOADED feed for hash with no regard for a /-filter, so a probe answers
// "is this commit in the feed at all" rather than "is it visible right now".
// The landing clears that filter (see steerClearCommitsFilter), and a probe
// that honoured it would refuse a commit gg really does hold.
func (m Model) steerCommitRow(hash string) (model.Commit, bool) {
	for u := 0; u < m.commitsTotal(); u++ {
		if c, ok := m.commitAtUnified(u); ok && commitIsHash(c, hash) {
			return c, true
		}
	}
	return model.Commit{}, false
}

// steerClearCommitsFilter drops a `/` filter bound to the Commits panel — and
// nothing else. "Go to" semantics: a filter that hides the target row would
// make the landing invisible. Deliberately NOT clearFilteringForFocus, which
// also drops the `@` highlight and the `\` commit-scope filter and would force
// a feed re-walk the agent never asked for.
func (m Model) steerClearCommitsFilter() Model {
	if m.filterPanel == panelCommits {
		m.filterQuery = ""
	}
	return m
}

// steerStatusRow finds the backing index of path in file panel p by MEMBERSHIP
// alone — no sort, no /-filter. The landing clears the filter anyway, and a
// lookup that honoured it would refuse a path the panel really does carry.
func (m Model) steerStatusRow(p panel, path string) (int, bool) {
	for i := range m.status.Files {
		if m.status.Files[i].Path == path && m.memberOf(p, i) {
			return i, true
		}
	}
	return -1, false
}

// steerNavigateStatusFile selects a working-tree/index row by path and opens
// its diff. retried says the one status reload has already happened, so a miss
// this time is final. Every refusal is decided BEFORE the view is moved.
func (m Model) steerNavigateStatusFile(c steer.Command, retried bool) (Model, tea.Cmd) {
	staged := c.Target != nil && c.Target.State == "staged"
	p := panelFiles
	if staged {
		p = panelStaged
	}

	bi, ok := m.steerStatusRow(p, c.File)
	if !ok {
		if retried {
			return m, m.answerSteer(c, steerFail(c, c.File+" is not in the working-tree diff"))
		}
		// The agent may have written the file a moment ago and this repo may
		// have no usable file-watch (drvfs): re-read status once before giving
		// up.
		m.pendingSteer = &pendingSteer{cmd: c, stage: steerStageStatusRetry, at: time.Now()}
		var cmd tea.Cmd
		m, cmd = m.reloadSourcesCmd([]sourceKey{srcStatus}, reloadOpts{})
		return m, cmd
	}
	f := m.status.Files[bi]
	if f.Kind == model.KindUnmerged {
		return m, m.answerSteer(c, steerFail(c, c.File+" is conflicted; open the conflict editor yourself"))
	}

	// Committed to the move from here on.
	m = m.steerToPanels()
	m = m.activateTab(p)
	// "Go to" semantics, exactly as gotoCommitByHash has them: a /-filter that
	// hides the row would make the landing invisible.
	m, _ = m.clearFilteringForFocus()
	for di, b := range m.displayIndices(p) {
		if b == bi {
			m.sel[p] = di
			break
		}
	}
	tm, cmd := m.openStatusDiff(f, staged)
	m = tm.(Model)
	if c.Line == nil {
		return m, tea.Batch(cmd, m.answerSteer(c, steerOK(c, "opened "+c.File)))
	}
	m.pendingSteer = &pendingSteer{cmd: c, stage: steerStageDiff, tag: m.diffTag, at: time.Now()}
	return m, cmd
}

// steerNavigateCommitFile lands on a loaded commit, opens its changed-file list
// and parks until that list arrives.
func (m Model) steerNavigateCommitFile(c steer.Command) (Model, tea.Cmd) {
	hash := c.Target.Commit
	if hash == "" {
		return m, m.answerSteer(c, steerFail(c, "target.state \"commit\" needs target.commit"))
	}
	if _, ok := m.steerCommitRow(hash); !ok { // probe before moving anything
		return m, m.answerSteer(c, steerFail(c, "commit not loaded in the feed"))
	}
	m = m.steerToPanels().steerClearCommitsFilter()
	m, commit, ok := m.gotoLoadedCommit(hash)
	if !ok {
		return m, m.answerSteer(c, steerFail(c, "commit not loaded in the feed"))
	}
	var cmd tea.Cmd
	m, cmd = m.openChangedFiles(commit)
	// Park the FEED's hash: openChangedFiles writes commit.Hash into
	// m.filesHash, and drainPendingFiles gates on m.filesHash == ps.hash. The
	// CLI's value resolved to the same commit but need not be the same string.
	m.pendingSteer = &pendingSteer{cmd: c, stage: steerStageFiles, hash: commit.Hash, at: time.Now()}
	return m, cmd
}

// drainPendingStatus retries the path lookup after the one status reload.
func (m Model) drainPendingStatus() (Model, tea.Cmd) {
	ps := m.pendingSteer
	if ps == nil || ps.stage != steerStageStatusRetry {
		return m, nil
	}
	// The reload is async: the user may have opened something in the meantime,
	// and the retry is about to move panels. Re-check the whitelist rather than
	// inheriting the one applySteer ran before the reload started.
	if why := m.steerRefusal(); why != "" {
		return m.failPending(why)
	}
	m.pendingSteer = nil
	return m.steerNavigateStatusFile(ps.cmd, true)
}

// drainPendingLoad is drainPendingFiles' and drainPendingPreview's shared
// body: the load that unblocks either stage is async, so the user may have
// moved since the pending was parked — re-check the whitelist rather than
// inherit the decision applySteer made before the load started. Then find the
// commanded path in the freshly loaded list, refuse a narrow terminal before
// opening a diff (openDiffForFileLine posts an i18n statusMsg instead, and a
// reply must be English protocol prose), and either land it now (no
// steer.Line) or advance the pending to the diff stage. opened and notFound
// are the two replies, already carrying the load's identity ("commit <hash>"
// / "preview <target>...<source>").
func (m Model) drainPendingLoad(c steer.Command, lines []contentLine, opened, notFound string) (Model, tea.Cmd) {
	if why := m.steerRefusal(); why != "" {
		return m.failPending(why)
	}
	for i, l := range lines {
		if l.heading || l.path != c.File {
			continue
		}
		if m.width > 0 && m.width < 60 {
			return m.failPending("the terminal is too narrow for the diff view")
		}
		m.filesView.sel = i
		tm, cmd := m.openDiffForFileLine(l)
		m = tm.(Model)
		if m.diffLayer() == nil {
			return m.failPending("the diff could not be opened")
		}
		if c.Line == nil {
			m.pendingSteer = nil
			return m, tea.Batch(cmd, m.answerSteer(c, steerOK(c, opened)))
		}
		m.pendingSteer = &pendingSteer{cmd: c, stage: steerStageDiff, tag: m.diffTag, at: time.Now()}
		return m, cmd
	}
	// A miss here is answered but NOT undone: the list loads late, so by the
	// time it arrives the view it belongs to is already open. There is nothing
	// to restore to — the agent named a path this load does not carry, and the
	// view that opened is a truthful answer to the half of the command that WAS
	// valid.
	return m.failPending(notFound)
}

// drainPendingFiles selects the commanded path in the freshly loaded file list
// and opens its diff, advancing the pending to the diff stage.
func (m Model) drainPendingFiles() (Model, tea.Cmd) {
	ps := m.pendingSteer
	if ps == nil || ps.stage != steerStageFiles || m.filesView == nil || m.filesHash != ps.hash {
		return m, nil
	}
	c := ps.cmd
	return m.drainPendingLoad(c, m.filesView.lines,
		"opened "+c.File+" in "+shortHash(ps.hash),
		c.File+" is not in commit "+shortHash(ps.hash))
}

// drainPendingDiff lands the cursor once the diff it was parked on has arrived.
// v is the view the diffMsg handler has already filled in.
func (m Model) drainPendingDiff(v *diffView) (Model, tea.Cmd) {
	ps := m.pendingSteer
	if ps == nil || ps.stage != steerStageDiff || ps.tag != m.diffTag {
		return m, nil
	}
	c := ps.cmd
	m.pendingSteer = nil
	if v.err != nil {
		return m, m.answerSteer(c, steerFail(c, "the diff failed to load: "+v.err.Error()))
	}
	return m.landSteer(v, c)
}

// landSteer puts the diff cursor on the commanded line: resolve the anchor
// (expanding a fold that hides it, clamping past the end of the file rather
// than refusing), then centre. This is gotoNote's recipe with a {side,no} in
// place of a note id.
//
// lineAnchor has THREE outcomes, and the fold one is not a miss: (i, true) is a
// visible line, (i, false) is the fold entry hiding an existing line — expand
// it and re-find, exactly as the note jump does — and (-1, false) is the only
// real "not in this diff", which is where the clamp belongs.
func (m Model) landSteer(v *diffView, c steer.Command) (Model, tea.Cmd) {
	old := c.Line.Side == "old"
	no := c.Line.No
	clamped := false
	find := func() (int, bool) { return v.lineAnchor(no, old) }

	li, _ := find()
	if li >= 0 {
		var ok bool
		if m, li, ok = m.expandFoldFor(v, li, find); !ok {
			// The fold covered a number the file does not actually reach (a
			// trailing fold and a number past the end): fall through to the
			// clamp, now against the EXPANDED view's real last line.
			li = -1
		}
	}
	if li < 0 {
		last := v.lastLineNo(old)
		if last == 0 {
			side := "new"
			if old {
				side = "old"
			}
			return m, m.answerSteer(c, steerFail(c, c.File+" has no "+side+" side in this diff"))
		}
		if no > last {
			no, clamped = last, true
			li, _ = find()
			if li >= 0 {
				var ok bool
				if m, li, ok = m.expandFoldFor(v, li, find); !ok {
					li = -1
				}
			}
		}
		if li < 0 {
			return m, m.answerSteer(c, steerFail(c, "line "+strconv.Itoa(c.Line.No)+" is not in "+c.File+"'s diff"))
		}
	}
	body := m.diffBodyRows()
	v.setCursorLine(li, body)
	v.alignCursor(alignCenter, body)

	m.diffNotice = i18n.T("▸ agent opened %s", c.File+":"+strconv.Itoa(no))

	detail := "opened " + c.File + ":" + strconv.Itoa(no)
	if clamped {
		detail += "; clamped to line " + strconv.Itoa(no)
	}
	return m, m.answerSteer(c, steerOK(c, detail))
}

// failPending answers and clears whatever is parked. Every failure branch of
// every load the pipeline waits on routes through here — a pending that is
// dropped without an answer leaves the CLI waiting out its two seconds and
// leaves the next unrelated load to land the cursor somewhere random.
func (m Model) failPending(reason string) (Model, tea.Cmd) {
	ps := m.pendingSteer
	if ps == nil {
		return m, nil
	}
	m.pendingSteer = nil
	return m, m.answerSteer(ps.cmd, steerFail(ps.cmd, reason))
}

// expirePendingSteer gives up on a parked command whose load never arrived
// (the user closed the view). Called from the heartbeat.
func (m Model) expirePendingSteer(now time.Time) (Model, tea.Cmd) {
	if m.pendingSteer == nil || now.Sub(m.pendingSteer.at) < steerPendingTTL {
		return m, nil
	}
	return m.failPending("the view did not load in time")
}

// steerNavigatePreview lands in a MERGE PREVIEW. With no file it only reveals
// the Previews entry; with one it opens the preview (a saved row when the
// pair has one, else a transient "show once" — the very open
// openPreviewPairDialog's own "show once" option performs) and parks until
// the compare file list arrives.
func (m Model) steerNavigatePreview(c steer.Command) (Model, tea.Cmd) {
	src, tgt := c.Target.Source, c.Target.Target
	// Which saved row, if any, holds this pair. "" means a show-once open.
	id, bi := "", -1
	for i, r := range m.previews {
		if r.rec.Source == src && r.rec.Target == tgt {
			id, bi = r.rec.ID, i
			break
		}
	}
	if c.File == "" {
		m = m.steerToPanels().activateTab(panelPreviews)
		if bi >= 0 {
			// "Go to" semantics, as everywhere else: a /-filter that hid the row
			// would make the landing invisible.
			m, _ = m.clearFilteringForFocus()
			for di, b := range m.displayIndices(panelPreviews) {
				if b == bi {
					m.sel[panelPreviews] = di
					break
				}
			}
		}
		m = m.steerNotice(i18n.T("▸ agent moved the focus"))
		return m, m.answerSteer(c, steerOK(c, "revealed preview "+tgt+"..."+src))
	}
	// Decided before the view moves: openDiffForFileLine refuses below 60
	// columns with an i18n status message, and a reply must be English prose.
	if m.width > 0 && m.width < 60 {
		return m, m.answerSteer(c, steerFail(c, "the terminal is too narrow for the diff view"))
	}
	// steerToPanels closes the files view, which clears previewOpen and bumps
	// previewGen — so the open below always really loads, instead of hitting
	// handlePreviewOpenMsg's same-tag reconcile arm (which issues no
	// compareFilesMsg and would leave this pending to expire).
	m = m.steerToPanels()
	m.pendingSteer = &pendingSteer{cmd: c, stage: steerStagePreview, source: src, target: tgt, at: time.Now()}
	return m, m.openPreviewCmd(id, src, tgt, "")
}

// drainPendingPreview selects the commanded path in the preview's freshly
// loaded compare list and opens its diff, advancing to the diff stage. It is
// drainPendingFiles' twin for the compare lane: openCompareFiles sets
// m.filesHash to one of the two ENDPOINT hashes, the same field a plain
// single-commit view uses, so a hash cannot tell a preview's list apart from
// an unrelated commit view that happens to share it — the open (source,
// target) PAIR is what actually identifies a preview, and that is the gate
// here.
func (m Model) drainPendingPreview() (Model, tea.Cmd) {
	ps, po := m.pendingSteer, m.previewOpen
	if ps == nil || ps.stage != steerStagePreview || m.filesView == nil || po == nil ||
		po.source != ps.source || po.target != ps.target {
		return m, nil
	}
	c := ps.cmd
	pair := ps.target + "..." + ps.source
	return m.drainPendingLoad(c, m.filesView.lines,
		"opened "+c.File+" in preview "+pair,
		c.File+" is not in preview "+pair)
}
