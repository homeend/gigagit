package tui

import (
	"strconv"
	"strings"
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
	steerStageCompare                       // a pair's compare file list (openCompareFiles)
)

// steerPendingTTL bounds a parked navigate. The user can close the view the
// command was waiting for, in which case its load never arrives; without a
// bound the pending would then land on an unrelated later diff.
const steerPendingTTL = 5 * time.Second

// pendingHint carries the navigate whose hint (spec §3.3) is being revealed
// across the async bookmark/shelf load, mirroring pendingCompare's staged-
// intent pattern (bookmark_compare.go). mustAnswer is true only for the
// hint-only shape (S13): its landing IS the reveal, so the async load's own
// present/absent finding is what answers the steer command. The with-address
// shape has already answered by the time this is staged (navigateLanded), so
// its absent case is a statusMsg notice only — never a second reply.
type pendingHint struct {
	cmd        steer.Command
	mustAnswer bool
	// tag and at are pendingSteer's own guard pattern (fix F3): tag is the
	// m.hintGen value stamped into THIS reveal's own load
	// (loadBookmarksForHintCmd/loadShelfForHintCmd), so a load an unrelated
	// `g`/`G` keypress already had in flight — which carries no tag a real
	// hint load would — can never be mistaken for this one's arrival (and,
	// symmetrically, this one's arrival is never swallowed by that load's
	// handler running first). at bounds how long a reveal parks on a load
	// that never arrives (a disabled store, a repo switch racing it).
	tag int
	at  time.Time
}

// pendingHintTTL mirrors steerPendingTTL: a hint reveal parked on a
// bookmark/shelf load that never arrives must not park forever.
const pendingHintTTL = 5 * time.Second

// expirePendingHint gives up on a parked hint reveal whose load never
// arrived. Called from the same heartbeat expirePendingSteer is (fix F3).
func (m Model) expirePendingHint(now time.Time) (Model, tea.Cmd) {
	ph := m.pendingHint
	if ph == nil || now.Sub(ph.at) < pendingHintTTL {
		return m, nil
	}
	m.pendingHint = nil
	if ph.mustAnswer {
		return m, m.answerSteer(ph.cmd, steerFail(ph.cmd, "the "+ph.cmd.HintKind+" list did not load in time"))
	}
	return m, nil
}

// pendingSteer is a navigate command parked until the load it needs arrives.
type pendingSteer struct {
	cmd   steer.Command
	stage steerStage
	// tag is the m.diffTag this landing belongs to (steerStageDiff) or the
	// m.compareTag the pair's file list is loading under (steerStageCompare)
	// -- never both at once, since a pendingSteer holds exactly one stage.
	tag    string
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
	// navigateLanded, not answerSteer directly (fix F4): a step navigate
	// carrying a hint must reveal too, the same as every other landing
	// shape — live.js's steerNavigate already does, because it wraps the
	// WHOLE verb rather than special-casing steerNavigateLand's own
	// `if (s.step) { …; return; }` arm, and this arm was the one the TUI
	// side had not routed through the chokepoint.
	return m.navigateLanded(c, "stepped to the "+what+" note")
}

// steerNavigate is the whole navigate verb. It resolves what it can
// synchronously and parks the rest.
func (m Model) steerNavigate(c steer.Command) (Model, tea.Cmd) {
	switch {
	case c.Step != "":
		return m.steerStep(c)
	case c.Target != nil && c.Target.State == "preview":
		return m.steerNavigatePreview(c)
	case c.Target != nil && c.Target.State == "ref":
		return m.steerNavigateRef(c)
	case c.Target != nil && c.Target.State == "pair":
		return m.steerNavigatePair(c)
	case c.File != "" && c.Target != nil && c.Target.State == "commit":
		return m.steerNavigateCommitFile(c)
	case c.File != "":
		return m.steerNavigateStatusFile(c, false)
	case c.HintKind != "" && c.Commit == "":
		// S13: a hint-only navigate — no File (excluded above), no Commit, no
		// Target (excluded above), no Step (excluded above). A link with no
		// address at all, whose landing IS the reveal (ruling S12: never
		// adopt the entry's own stored address).
		return m.steerNavigateHintOnly(c)
	case c.Commit != "":
		// Probe first: gotoLoadedCommit (and steerToPanels) MOVE the view, and
		// a commit the feed has not paged in is a refusal, not a reason to
		// close what the user was reading.
		if _, ok := m.steerCommitRow(c.Commit); !ok {
			if startAtOrigin(c) {
				// The user's own link (gg open, a pasted link): a commit the
				// feed has not paged in is still a commit gg can show — open
				// its files by hash, exactly what `#` does for a typed sha.
				// An agent's navigate keeps the refusal: it asked for a feed
				// row, and moving the user into a files view is not that.
				nm := m.steerToPanels()
				nm, cmd := nm.openChangedFiles(model.Commit{Hash: c.Commit})
				nm.focus = panelCommits
				nm = nm.focusTree()
				nm.statusMsg = i18n.T("▸ opened %s", shortHash(c.Commit))
				rm, rcmd := nm.navigateLanded(c, "opened "+shortHash(c.Commit))
				return rm, tea.Batch(cmd, rcmd)
			}
			return m, m.answerSteer(c, steerFail(c, "commit not loaded in the feed"))
		}
		nm := m.steerToPanels().steerClearCommitsFilter()
		nm, _, ok := nm.gotoLoadedCommit(c.Commit)
		if !ok {
			return m, m.answerSteer(c, steerFail(c, "commit not loaded in the feed"))
		}
		if startAtOrigin(c) {
			// The user's own link (gg open, or pasted into #): a steered reveal
			// answers its CLI, but nobody answers the user — and a commit that
			// was already selected would otherwise look like nothing happened.
			nm.statusMsg = i18n.T("▸ opened %s", shortHash(c.Commit))
		}
		return nm.navigateLanded(c, "revealed commit "+shortHash(c.Commit))
	}
	return m, m.answerSteer(c, steerFail(c, "navigate needs a file, a commit or a step"))
}

// navigateLanded is the ONE place a navigate's own success is recorded (now
// true of every shape, including the step verb — fix F4): it answers the
// command — the navigate's success is whether it LANDED, never whether its
// hint could be shown (spec §3.3 rule 3) — and, when c carries a hint,
// stages the post-landing REVEAL: a bookmark/shelf popup with that row
// selected, or a notice for a kind this build has no reveal for (S9: the
// default degrades, it never silently drops). Every landing site that used
// to build steerOK directly calls this instead, so the hint cannot end up
// wired into only SOME of them — the S2/S5 defect shape this plan keeps
// hitting. Each stage bumps m.hintGen and stamps it into the reveal's own
// load (fix F3): a `g`/`G` press already in flight when this lands must not
// be mistaken for this reveal's arrival, nor swallow it.
func (m Model) navigateLanded(c steer.Command, detail string) (Model, tea.Cmd) {
	reply := m.answerSteer(c, steerOK(c, detail))
	switch c.HintKind {
	case "":
		return m, reply
	case "bookmark":
		m.hintGen++
		m.pendingHint = &pendingHint{cmd: c, tag: m.hintGen, at: time.Now()}
		return m, tea.Batch(reply, m.loadBookmarksForHintCmd(m.hintGen))
	case "shelf":
		m.hintGen++
		m.pendingHint = &pendingHint{cmd: c, tag: m.hintGen, at: time.Now()}
		return m, tea.Batch(reply, m.loadShelfForHintCmd(c.HintID, m.hintGen))
	default:
		// "stash" (spec §3.4, no producer) and any future kind this build
		// cannot reveal: the navigate already landed, so this degrades with
		// a notice rather than touching the reply.
		m.statusMsg = i18n.T("that link's hint has no landing gg can show")
		return m, reply
	}
}

// steerNavigateHintOnly lands a hint-only navigate (S13): a link with no
// address at all, whose landing IS the reveal. Domain has already checked
// presence for this shape before a Command could even be built (ruling S11
// — resolveLink hard-errors an absent address-less hint at resolve time), so
// kind is bookmark or shelf in every real path; the default below stays an
// explicit refusal (S9) rather than a silent no-op for whatever reaches here
// defensively (e.g. a hand-crafted command bypassing the normal producers).
func (m Model) steerNavigateHintOnly(c steer.Command) (Model, tea.Cmd) {
	switch c.HintKind {
	case "bookmark":
		m.hintGen++
		m.pendingHint = &pendingHint{cmd: c, mustAnswer: true, tag: m.hintGen, at: time.Now()}
		return m, m.loadBookmarksForHintCmd(m.hintGen)
	case "shelf":
		m.hintGen++
		m.pendingHint = &pendingHint{cmd: c, mustAnswer: true, tag: m.hintGen, at: time.Now()}
		return m, m.loadShelfForHintCmd(c.HintID, m.hintGen)
	default:
		return m, m.answerSteer(c, steerFail(c, "that link's "+c.HintKind+" hint has no landing gg can show"))
	}
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
		rm, rcmd := m.navigateLanded(c, "opened "+c.File)
		return rm, tea.Batch(cmd, rcmd)
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

// steerNavigateRef lands a @ref:<name> navigate. The wire carries the NAME
// (ruling R2), so the tip is resolved HERE: a branch that moved between post
// and apply lands on the new tip. Once resolved it is an ordinary commit
// navigate, so it delegates rather than duplicating either landing.
func (m Model) steerNavigateRef(c steer.Command) (Model, tea.Cmd) {
	name := c.Target.Ref
	if name == "" {
		return m, m.answerSteer(c, steerFail(c, "target.state \"ref\" needs target.ref"))
	}
	// A DEADLINED read on the Update thread: the ff-pull lane does not set
	// m.running, so a background fetch can hold the gate with opsIdle() still
	// true. updateThreadCtx is the seam plan 1b added for exactly this.
	ctx, cancel := updateThreadCtx(updateThreadGitTimeout)
	defer cancel()
	sha, ok, err := m.svc.ResolveRev(ctx, name)
	if err := busyOr(err); err != nil {
		return m, m.answerSteer(c, steerFail(c, "resolving "+name+": "+err.Error()))
	}
	if !ok {
		return m, m.answerSteer(c, steerFail(c, name+" does not resolve here"))
	}
	hash := strings.TrimSpace(sha)
	nc := c
	nc.Target = &steer.Target{State: "commit", Commit: hash}
	if nc.File != "" {
		// NOT steerNavigateCommitFile: its first move is a probe of the
		// ALREADY-LOADED feed that refuses "commit not loaded in the feed",
		// and a branch tip is the commit least likely to be paged in — the
		// very reason the file-less arm below opens by hash. Delegating here
		// made the two halves of ONE lane disagree: no file landed, a file
		// refused. Open by hash and park on the file list, which is what
		// steerNavigateCommitFile does AFTER its probe.
		nm := m.steerToPanels()
		nm, cmd := nm.openChangedFiles(model.Commit{Hash: hash})
		// hash, not a feed row's Hash: openChangedFiles writes exactly this
		// string into m.filesHash, and drainPendingFiles gates on equality
		// with it.
		nm.pendingSteer = &pendingSteer{cmd: nc, stage: steerStageFiles, hash: hash, at: time.Now()}
		return nm, cmd
	}
	// NOT steerNavigate(nc) with nc.Commit set: that arm refuses an agent's
	// navigate for a commit the feed has not paged in, and a branch tip is the
	// commit least likely to be paged in. A ref names a TREE, so open it by
	// hash for either origin (see the landing note above).
	nm := m.steerToPanels()
	nm, cmd := nm.openChangedFiles(model.Commit{Hash: hash})
	nm.focus = panelCommits
	nm = nm.focusTree()
	if startAtOrigin(c) {
		nm = nm.steerNotice(i18n.T("▸ opened %s", name+" at "+shortHash(hash)))
	}
	rm, rcmd := nm.navigateLanded(c, "opened "+name+" at "+shortHash(hash))
	return rm, tea.Batch(cmd, rcmd)
}

// steerNavigatePair lands a @<a>..<b> navigate: a change-set is BOUNDED, so
// it is a COMPARISON, and the compare files view is where a comparison
// lives. The halves arrive as names or shas and are resolved here for the
// same reason a ref is (ruling R2): a moving name must be re-resolved at
// apply time, never taken frozen off the wire.
//
// It lands through domain.CompareLinks — commit a's whole tree against the
// change-set a..b — the same door `gg compare <link>` answers through, so the
// TUI and the CLI cannot disagree about one link. That is what shows a `-u`
// stash's untracked files here: they are members of the SET (read from the
// stash's third parent), and no two-commit tree diff contains them.
//
// The view's two endpoints are still commit a and commit b, never a
// PairEndpoint: a pair set's own endpoint is its b side.
func (m Model) steerNavigatePair(c steer.Command) (Model, tea.Cmd) {
	a, b := c.Target.A, c.Target.B
	if a == "" || b == "" {
		return m, m.answerSteer(c, steerFail(c, "target.state \"pair\" needs target.a and target.b"))
	}
	// Both halves resolved under ONE deadline: the same Update-thread hazard
	// steerNavigateRef guards against.
	ctx, cancel := updateThreadCtx(updateThreadGitTimeout)
	defer cancel()
	ash, aok, aerr := m.svc.ResolveRev(ctx, a)
	if err := busyOr(aerr); err != nil {
		return m, m.answerSteer(c, steerFail(c, "resolving "+a+": "+err.Error()))
	}
	if !aok {
		return m, m.answerSteer(c, steerFail(c, a+" does not resolve here"))
	}
	bsh, bok, berr := m.svc.ResolveRev(ctx, b)
	if err := busyOr(berr); err != nil {
		return m, m.answerSteer(c, steerFail(c, "resolving "+b+": "+err.Error()))
	}
	if !bok {
		return m, m.answerSteer(c, steerFail(c, b+" does not resolve here"))
	}
	ahash, bhash := strings.TrimSpace(ash), strings.TrimSpace(bsh)
	left, err := model.CommitEndpoint(ahash)
	if err != nil {
		return m, m.answerSteer(c, steerFail(c, "resolving "+a+": "+err.Error()))
	}
	right, err := model.CommitEndpoint(bhash)
	if err != nil {
		return m, m.answerSteer(c, steerFail(c, "resolving "+b+": "+err.Error()))
	}

	m = m.steerToPanels()
	if leftText, lok := m.pointLinkFor(ahash); lok {
		if rightText, rok := m.pairLinkFor(ahash, bhash); rok {
			// Parked whether or not a file was named: the view does not exist
			// until the comparison lands, so the reply (and the notice) wait
			// for loadedLinkCompare rather than claiming an open that may fail.
			nm, cmd := m.startLinkCompare(leftText, rightText)
			nm.pendingSteer = &pendingSteer{cmd: c, stage: steerStageCompare, tag: linkCompareTag(leftText, rightText), at: time.Now()}
			if cmd == nil { // already showing this very comparison
				return nm.drainPendingCompare()
			}
			return nm, cmd
		}
	}
	// No link form for this checkout (its path holds a character the grammar
	// cannot): the endpoint-shaped compare, which is right for every pair
	// except a `-u` stash's.
	nm, cmd := m.openCompareFiles(left, right)
	pair := shortHash(ahash) + ".." + shortHash(bhash)
	if startAtOrigin(c) {
		nm = nm.steerNotice(i18n.T("▸ opened %s..%s", shortHash(ahash), shortHash(bhash)))
	}
	if c.File == "" {
		rm, rcmd := nm.navigateLanded(c, "opened "+pair)
		return rm, tea.Batch(cmd, rcmd)
	}
	// Park the COMPARE's tag: drainPendingCompare gates on it, never on
	// m.filesHash (drainPendingFiles' gate) — openCompareFiles happens to also
	// set m.filesHash from one side's hash, but compareFilesMsg's handler
	// never routes a compare's success through drainPendingFiles, so a
	// pending parked there would never drain.
	nm.pendingSteer = &pendingSteer{cmd: c, stage: steerStageCompare, tag: nm.compareTag, at: time.Now()}
	return nm, cmd
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
			rm, rcmd := m.navigateLanded(c, opened)
			return rm, tea.Batch(cmd, rcmd)
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

// drainPendingCompare selects the commanded path in a pair's freshly loaded
// compare file list and opens its diff, advancing to the diff stage. It is
// drainPendingFiles' and drainPendingPreview's twin for the change-set lane:
// compareFilesMsg (not commitFilesMsg) fills a pair's file list, and its
// handler never routes success through drainPendingFiles, so a pending
// parked on m.filesHash (drainPendingFiles' gate) would never drain — gate on
// m.compareTag instead, the identity a two-sided view actually carries.
func (m Model) drainPendingCompare() (Model, tea.Cmd) {
	ps := m.pendingSteer
	if ps == nil || ps.stage != steerStageCompare || m.filesView == nil || ps.tag != m.compareTag {
		return m, nil
	}
	c := ps.cmd
	pair := c.Target.A + ".." + c.Target.B
	// A link-shaped landing could not say so at dispatch — its view did not
	// exist yet — so the notice is raised here, once the list is in hand.
	if m.filesSets != nil && startAtOrigin(c) {
		m = m.steerNotice(i18n.T("▸ opened %s..%s", shortHash(m.filesLeft.Hash()), shortHash(m.filesRight.Hash())))
	}
	if c.File == "" {
		// A link-shaped landing parks even a file-less navigate; its list
		// being here IS the landing.
		m.pendingSteer = nil
		return m.navigateLanded(c, "opened "+shortHash(m.filesLeft.Hash())+".."+shortHash(m.filesRight.Hash()))
	}
	return m.drainPendingLoad(c, m.filesView.lines,
		"opened "+c.File+" in "+pair,
		c.File+" is not in "+pair)
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

	if startAtOrigin(c) {
		m.diffNotice = i18n.T("▸ opened %s", c.File+":"+strconv.Itoa(no))
	} else {
		m.diffNotice = i18n.T("▸ agent opened %s", c.File+":"+strconv.Itoa(no))
	}

	detail := "opened " + c.File + ":" + strconv.Itoa(no)
	if clamped {
		detail += "; clamped to line " + strconv.Itoa(no)
	}
	return m.navigateLanded(c, detail)
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
		if startAtOrigin(c) {
			m = m.steerNotice(i18n.T("▸ opened preview %s", tgt+"..."+src))
		} else {
			m = m.steerNotice(i18n.T("▸ agent moved the focus"))
		}
		return m.navigateLanded(c, "revealed preview "+tgt+"..."+src)
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

// steerCommandForLink turns a FULLY RESOLVED link into the navigate command
// the CLI would have posted for it — the `--at` startup path's only step.
//
// It is pure: no git, no service. That is why a #<hunk> link is a REFUSAL
// rather than something silently dropped — `gg open` lowers a hunk to a line
// (through PreviewHunkAnchor / HunkRange, on the target checkout) before it
// ever reaches the launcher, so a hunk arriving here means the caller skipped
// that step.
func steerCommandForLink(l model.Link) (steer.Command, bool) {
	if l.Hunk > 0 {
		return steer.Command{}, false
	}
	c := steer.Command{Cmd: "navigate"}
	// Set ONCE, before any of this function's several early returns (ruling
	// S2's lesson applied to a second function): every arm below returns
	// this same c, so the hint rides whichever shape the link turns out to
	// be.
	c.HintKind, c.HintID = l.Hint.Kind, l.Hint.ID
	if p := l.Target.Preview; p != nil {
		c.Target = &steer.Target{State: "preview", Source: p.Source, Target: p.Target}
		if l.Path == "" {
			return c, true
		}
		c.File = l.Path
		if l.Line > 0 {
			c.Line = &steer.Line{Side: "new", No: l.Line} // a preview has no old side
		}
		return c, true
	}
	if name := l.Target.Ref; name != "" {
		// A tip's NAME rides the wire (ruling R2): the started TUI resolves it
		// itself, in steerNavigateRef, the same place a live session does.
		c.Target = &steer.Target{State: "ref", Ref: name}
		if l.Path == "" {
			return c, true
		}
		c.File = l.Path
		if l.Line > 0 {
			side := "new"
			if l.Side == model.NoteSideOld {
				side = "old"
			}
			c.Line = &steer.Line{Side: side, No: l.Line}
		}
		return c, true
	}
	if p := l.Target.Pair; p != nil {
		// Each half may be a sha or a refname (model.LinkPair) and rides the
		// wire as-is: steerNavigatePair resolves both at apply time.
		c.Target = &steer.Target{State: "pair", A: p.A, B: p.B}
		if l.Path == "" {
			return c, true
		}
		c.File = l.Path
		if l.Line > 0 {
			side := "new"
			if l.Side == model.NoteSideOld {
				side = "old"
			}
			c.Line = &steer.Line{Side: side, No: l.Line}
		}
		return c, true
	}
	if l.Path == "" {
		// A commit reveal needs the FULL sha: the consumer compares hashes.
		if l.Target.State == model.StateCommitted && len(l.Target.Commit) >= 40 {
			c.Commit = l.Target.Commit
			return c, true
		}
		if l.Target.Commit == "" && l.Hint.Kind != "" {
			// S13: a hint-only link — no path, no commit at all — whose
			// landing IS the reveal.
			return c, true
		}
		return steer.Command{}, false
	}
	t := &steer.Target{State: "unstaged"}
	switch l.Target.State {
	case model.StateStaged:
		t.State = "staged"
	case model.StateCommitted:
		if len(l.Target.Commit) < 40 {
			return steer.Command{}, false
		}
		t.State, t.Commit = "commit", l.Target.Commit
	}
	c.File, c.Target = l.Path, t
	if l.Line > 0 {
		side := "new"
		if l.Side == model.NoteSideOld {
			side = "old"
		}
		c.Line = &steer.Line{Side: side, No: l.Line}
	}
	return c, true
}

// startAtMsg feeds the --at startup link into the steering pipeline on the
// Update goroutine, once every startAtReady precondition has landed.
type startAtMsg struct{ cmd steer.Command }

// startAtOrigin reports whether c is a navigate the USER initiated — the one
// steerCommandForLink synthesized for --at (`gg open`), or the one the #
// prompt built for a pasted link — rather than one a real steer.Post client
// sent. sendSteer always assigns an id before Post (and
// carries the caller's --wait choice), so a real client's command never
// arrives with both fields at their zero value; steerCommandForLink never
// sets either. Distinguishing the two lets the landing notice say "opened",
// not "agent opened" — an agent didn't drive this.
func startAtOrigin(c steer.Command) bool {
	return c.ID == "" && !c.Wait
}

// startAtReady reports whether every precondition for consuming --at has
// landed: SOME data has arrived (m.ready — guards the window before the
// startup fan-out has even begun; set by both the modern per-source
// dataAvailableMsg arrival and the legacy dataLoadedMsg), a window size
// (m.width — the diff-open path and steerNavigatePreview's width guard both
// need it), and ops idle (!m.opsIdle() mirrors applySteer's own
// steerRefusal gate exactly: !m.running && !m.loading — a navigate is
// refused while EITHER is true, and the real startup path (bootstrapCmd →
// configReadyMsg → reloadAllCmd) holds m.loading true until every one of
// the ~10 fanned-out sources has landed, not merely the first), plus — for
// a PREVIEW link only — the previews read: steerNavigatePreview resolves
// saved rows from m.previews, which a dedicated (possibly LATER,
// out-of-band) srcPreviews read fills in. A non-preview link never needs
// that read, so it is not awaited on its own — though in practice, at real
// startup, previews rides the same fan-out as everything else opsIdle
// already waits for.
//
// Checked centrally in Update, after every dispatch (not at each
// precondition's own handler): opsIdle flips true only once whichever
// source/op happens to finish LAST, so a per-handler check would fire while
// a sibling source was still loading and land in an unretried refusal.
func (m Model) startAtReady() bool {
	if !m.startAtPending || !m.ready || !m.opsIdle() || m.width == 0 {
		return false
	}
	if m.startAt.Target.Preview != nil {
		return m.startAtPreviewsSeen
	}
	return true
}

// consumeStartAt turns the --at link into the same navigate steerNavigate
// would run for a steered command — startAtMsg carries it back through
// Update so it takes the EXACT path a CLI-posted navigate does — or, if the
// link names no place gg can open, a status message. Either way
// startAtPending is cleared, so this fires exactly once.
func (m Model) consumeStartAt() (Model, tea.Cmd) {
	m.startAtPending = false
	c, ok := steerCommandForLink(m.startAt)
	if !ok {
		m.statusMsg = i18n.T("that gg link names no place gg can open")
		return m, nil
	}
	return m, func() tea.Msg { return startAtMsg{cmd: c} }
}
