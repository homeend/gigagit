package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clipboard"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/preflight"
)

// The notification center: cheap health checks run on every repo load
// (domain.RepoHealth); unresolved findings become notices — a blinking red
// status segment plus the ! dialog whose actions fix the finding through
// real engine ops. Notice lifecycle: unread → read (opening the dialog) →
// dismissed for the session ("Not now"; re-evaluated next load) or never for
// this repo (persisted via promptstate.DismissNotice, keyed by common dir).

// notice is one surfaced health recommendation.
type notice struct {
	id      string   // stable dismissal key (persisted on "never")
	repoKey string   // git common dir — the promptstate dismissal scope
	title   string   // one-line list entry
	detail  []string // body lines shown above the actions
	actions []noticeAction
}

// noticeAction is one dialog choice. run nil = close-only ("Not now");
// never additionally persists the per-repo dismissal.
type noticeAction struct {
	label string
	run   func(Model) (Model, tea.Cmd)
	never bool
}

// noticeCommitGraph is the commit-graph recommendation's stable id.
const noticeCommitGraph = "commit_graph_recommend"

// noticeClipboard is the "install a clipboard tool" recommendation's stable id.
const noticeClipboard = "clipboard_tool_missing"

// noticeNarrowRefspec is the "branches aren't tracked by the fetch refspec"
// recommendation's stable id.
const noticeNarrowRefspec = "narrow_fetch_refspec"

// noticeWSLInterop is the "WSL interop is unregistered, so copy is broken"
// warning's stable id.
const noticeWSLInterop = "wsl_interop_broken"

// noticeStaleLock is the "a git process left lockfiles behind" recovery
// notice's stable id. Unlike the other notices this one is a BLOCKER, not a
// recommendation: while the lock is there every operation touching that file
// fails, so it is also raised reactively (see maybeStaleLockNotice) rather
// than only on repo load.
const noticeStaleLock = "stale_git_lock"

// noticeBranchDriftPrefix identifies a post-op drift/paused-resume notice's
// id. Unlike the standing health notices above (one constant id, re-derived
// from repoHealth on every rebuild), each of these is a one-shot EVENT tied
// to the version ref DriftAfter compared against — see driftNoticeID.
const noticeBranchDriftPrefix = "branch_drift_"

// bigRepoPackBytes is the pack-size floor for "big repo": below it the
// commit-graph win doesn't matter enough to nag about.
const bigRepoPackBytes = 100 << 20

// Blink = style alternation between st().noticeHot and st().noticeDim on a
// dedicated tick; terminal-native blink escapes are unreliable.

// repoHealthMsg carries one background health read; gen guards repo switches.
// clipAvail rides along: it is an environment probe (not a git read), but it
// is cheap and belongs to the same load event that rebuilds the notice list.
type repoHealthMsg struct {
	gen       int
	health    model.RepoHealth
	clipAvail clipboard.Availability
	err       error
}

// repoHealthCmd reads repo health off the UI thread (startup, reRoot, and
// whenever Settings opens so its Commit-graph row shows fresh state). It also
// probes clipboard availability there so the PATH/socket lookups stay off the
// UI thread and reflect a tool installed mid-session on the next load.
func (m Model) repoHealthCmd(gen int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		h, err := svc.RepoHealth(context.Background())
		return repoHealthMsg{gen: gen, health: h, clipAvail: clipboard.Probe(), err: err}
	}
}

// noticeBlinkMsg flips the blink phase while unread notices exist. gen ties
// the tick to the arm that scheduled it — a stale gen (superseded by a later
// arm, e.g. across a reRoot or an unread→read→new-notice flip within one
// 800ms window) is dropped instead of re-arming a second, parallel lane.
type noticeBlinkMsg struct{ gen int }

// noticeBlinkCmd schedules the next blink flip (~800ms; only re-armed while
// unread notices exist, so the tick self-stops).
func noticeBlinkCmd(gen int) tea.Cmd {
	return tea.Tick(800*time.Millisecond, func(time.Time) tea.Msg { return noticeBlinkMsg{gen: gen} })
}

// applyRepoHealth is the repoHealthMsg Update case: store the snapshot,
// rebuild the notice list (filtered by persisted + session dismissals), and
// start blinking only when a genuinely NEW notice id appeared — a mid-session
// re-read carrying the same ids must not re-blink.
func (m Model) applyRepoHealth(msg repoHealthMsg) (Model, tea.Cmd) {
	if msg.gen != m.noticeGen {
		return m, nil // stale: a repo switch superseded this read
	}
	if msg.err != nil {
		return m, nil // best-effort: health never surfaces errors in the UI
	}
	m.repoHealth = msg.health
	m.repoHealthKnown = true
	// The common dir is the branch-filter promptstate key, and this is where it
	// first resolves — the load that normally sticks (bfSlotsLoaded keeps later
	// probes from re-reading and clobbering an alt+N made since).
	m = m.loadBranchFilterSlots()
	m.clipAvail = msg.clipAvail

	prev := make(map[string]bool, len(m.notices))
	for _, n := range m.notices {
		prev[n.id] = true
	}
	m = m.rebuildNotices()
	var cmd tea.Cmd
	for _, n := range m.notices {
		if !prev[n.id] {
			if !m.noticesUnread {
				m.blinkGen++
				cmd = noticeBlinkCmd(m.blinkGen)
			}
			m.noticesUnread = true
			m.blinkOn = true
			break
		}
	}
	return m, cmd
}

// rebuildNotices re-derives m.notices from the cached health snapshot, plus
// any post-op drift findings (m.driftNotices — see driftNotice). Notice
// titles/details/action labels bake i18n.T output at build time, so a
// language switch must rebuild them; ids are stable, so dismissals hold and
// no blink logic runs here (applyRepoHealth/applyDriftReport own blinking).
func (m Model) rebuildNotices() Model {
	var next []notice
	if m.repoHealthKnown {
		next = m.rebuildHealthNotices()
	}
	// Drift findings are re-rendered here too (not appended once and left as
	// a raw notice): a health re-read or a language switch reassigns
	// m.notices wholesale above, and re-deriving is the only way a switch
	// re-translates them and a health refresh doesn't silently drop them.
	// They never offer "Never for this repo" (a one-shot event has nothing
	// to persist), so only the session dismissal map applies.
	for _, d := range m.driftNotices {
		if n := driftNotice(d.branch, d.report, d.paused, m.repoHealth.GitCommonDir); n != nil && !m.noticeSessionDismissed[n.id] {
			next = append(next, *n)
		}
	}
	m.notices = next
	return m
}

// rebuildHealthNotices is rebuildNotices' health-derived half, split out so
// the drift-notice loop above can run even before the first health read
// lands (repoHealthKnown false) without duplicating that loop inside this
// one's early return.
func (m Model) rebuildHealthNotices() []notice {
	var dismissed map[string]bool
	if m.promptStore != nil {
		dismissed = m.promptStore.DismissedNotices(m.repoHealth.GitCommonDir)
	}
	var next []notice
	// Stale locks first: they block every subsequent operation, so they
	// outrank any advisory below them in the list.
	if n := staleLockNotice(m.repoHealth, time.Now()); n != nil && !dismissed[n.id] && !m.noticeSessionDismissed[n.id] {
		next = append(next, *n)
	}
	if n := commitGraphNotice(m.repoHealth); n != nil && !dismissed[n.id] && !m.noticeSessionDismissed[n.id] {
		next = append(next, *n)
	}
	if n := narrowRefspecNotice(m.repoHealth); n != nil && !dismissed[n.id] && !m.noticeSessionDismissed[n.id] {
		next = append(next, *n)
	}
	if n := clipboardNotice(m.clipAvail, m.repoHealth.GitCommonDir); n != nil && !dismissed[n.id] && !m.noticeSessionDismissed[n.id] {
		next = append(next, *n)
	}
	if n := wslInteropNotice(m.clipAvail, m.repoHealth.GitCommonDir); n != nil && !dismissed[n.id] && !m.noticeSessionDismissed[n.id] {
		next = append(next, *n)
	}
	for _, n := range featureDisabledNotices(m.svc, m.repoHealth.GitCommonDir) {
		if !dismissed[n.id] && !m.noticeSessionDismissed[n.id] {
			next = append(next, n)
		}
	}
	return next
}

// noticeFeatureDisabledPrefix identifies a disabled-feature notice's stable
// id, suffixed with the feature's English protocol id — dismissal is scoped
// per feature, so fixing one feature never silences another's notice.
const noticeFeatureDisabledPrefix = "feature_disabled_"

// featureDisabledNotices returns one standing notice per feature whose
// preflight verdict is not Satisfied. A Required-Unsatisfiable feature never
// reaches here — Preflight (internal/tui/preflight.go) refuses to launch the
// UI at all in that case — so what shows up is an Optional feature gg quietly
// ran without (Unsatisfiable), or one the user chose to Skip at the migration
// prompt (Repairable). svc.Preflight re-validates its cache against the repo
// on every call (one cheap for-each-ref for the store markers), so this call
// shells out but stays cheap on every rebuild.
func featureDisabledNotices(svc *domain.Service, repoKey string) []notice {
	vs, err := svc.Preflight(context.Background())
	if err != nil {
		return nil // best-effort, like every other health-derived notice
	}
	var out []notice
	for _, v := range vs {
		if v.State == preflight.Satisfied {
			continue
		}
		out = append(out, noticeForVerdict(v, repoKey))
	}
	return out
}

// noticeForVerdict builds the standing notice for one non-Satisfied verdict.
// Repairable gets the `gg migrate` line; Unsatisfiable gets the verdict's
// remedy (what fixes it from outside gg — upgrade git, upgrade gg). Either
// way "Never for this repo" stays available: this is an ordinary
// notification, dismissed like any other.
func noticeForVerdict(v preflight.Verdict, repoKey string) notice {
	detail := []string{renderVerdictReason(v)}
	switch v.State {
	case preflight.Repairable:
		detail = append(detail, i18n.T("Run `gg migrate` to repair this, or answer the migration prompt at the next launch."))
	case preflight.Unsatisfiable:
		detail = append(detail, renderVerdictRemedy(v))
	}
	return notice{
		id:      noticeFeatureDisabledPrefix + v.Feature.ID,
		repoKey: repoKey,
		title:   i18n.T("%s is unavailable in this repository", v.Feature.ID),
		detail:  detail,
		actions: []noticeAction{
			{label: i18n.T("Not now (ask again next load)")},
			{label: i18n.T("Never for this repo"), never: true},
		},
	}
}

// commitGraphNotice fires when the repo is big (pack ≥ bigRepoPackBytes), has
// no commit-graph file/chain, and fetch.writeCommitGraph is unset — the case
// where one keystroke makes commit browsing ~10× faster.
func commitGraphNotice(h model.RepoHealth) *notice {
	if h.PackBytes < bigRepoPackBytes || h.HasCommitGraph || h.WriteCommitGraphSet {
		return nil
	}
	return &notice{
		id:      noticeCommitGraph,
		repoKey: h.GitCommonDir,
		title:   i18n.T("Commit browsing can be ~10× faster in this repo"),
		detail: []string{
			i18n.T("This repo is big (%.0f MB of packs) and has no commit-graph file, so ordered commit walks (the Commits panel's paging) re-walk history every time.", float64(h.PackBytes)/(1<<20)),
			i18n.T("Writing one takes a moment and git keeps it fresh when fetch.writeCommitGraph is on."),
		},
		actions: []noticeAction{
			{label: i18n.T("Write commit-graph now + keep it fresh (fetch.writeCommitGraph=true)"),
				run: Model.startCommitGraphWriteAndEnable},
			{label: i18n.T("Enable auto-refresh only (graph appears on next fetch/gc)"),
				run: func(m Model) (Model, tea.Cmd) {
					m.refreshHealthAfterOp = true
					return m.startOp(engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"})
				}},
			{label: i18n.T("Not now (ask again next load)")},
			{label: i18n.T("Never for this repo"), never: true},
		},
	}
}

// narrowRefspecNotice fires when local branches have a configured upstream
// the fetch refspec cannot resolve (single-branch/shallow clones): pushes
// never move their remote-tracking refs, so the Commits panel's ↓↑ tip
// markers and ahead/behind silently stay stale. The fix action adds a
// per-branch mapping and fetches ONLY those branches — never the wildcard,
// which could trigger a mass download on a monorepo remote.
func narrowRefspecNotice(h model.RepoHealth) *notice {
	if len(h.UnmappedBranches) == 0 {
		return nil
	}
	title := i18n.T("%d branches aren't tracked by the fetch refspec", len(h.UnmappedBranches))
	if len(h.UnmappedBranches) == 1 {
		title = i18n.T("1 branch isn't tracked by the fetch refspec")
	}
	return &notice{
		id:      noticeNarrowRefspec,
		repoKey: h.GitCommonDir,
		title:   title,
		detail: []string{
			i18n.T("This clone's fetch refspec doesn't map these branches, so a push never moves their remote-tracking ref — the ↓↑ tip markers and ahead/behind cannot follow them: %s", strings.Join(h.UnmappedBranches, ", ")),
			i18n.T("gg can add a per-branch mapping and fetch just those branches (no mass download)."),
		},
		actions: []noticeAction{
			{label: i18n.T("Add mappings + fetch these branches"),
				run: func(m Model) (Model, tea.Cmd) {
					m.refreshHealthAfterOp = true
					return m.startOp(engine.AddFetchMappings{Remote: "origin", Branches: m.repoHealth.UnmappedBranches})
				}},
			{label: i18n.T("Not now (ask again next load)")},
			{label: i18n.T("Never for this repo"), never: true},
		},
	}
}

// staleLockNotice fires whenever a git lockfile is present. Git removes its
// own locks on exit — including when gg cancels it, which now terminates
// gracefully — so one still sitting there means a git died hard: a machine
// that lost power, a `kill -9`, a crashed tool, or gg itself on Windows,
// where cancellation cannot be graceful.
//
// The notice deliberately does NOT claim the lock is definitely stale: a git
// running right now (started from a terminal, an IDE, a hook) legitimately
// holds one, and deleting it would corrupt that write. gg cannot see
// processes it did not start, so it reports what it found — with each lock's
// age, the one usable staleness signal — and lets the human decide.
func staleLockNotice(h model.RepoHealth, now time.Time) *notice {
	if len(h.StaleLocks) == 0 {
		return nil
	}
	title := i18n.T("%d git lock files are present — operations will fail", len(h.StaleLocks))
	if len(h.StaleLocks) == 1 {
		title = i18n.T("A git lock file is present — operations will fail")
	}
	detail := []string{
		i18n.T("git creates a lock file while it rewrites the file beside it and removes it on exit, so one left behind means a git process was killed before it could clean up."),
		i18n.T("Until it is removed, git refuses to run with \"Another git process seems to be running in this repository\"."),
	}
	for _, l := range h.StaleLocks {
		detail = append(detail, "  "+l.Name+"  ("+lockAgeLabel(now.Sub(l.ModTime))+")")
	}
	detail = append(detail, i18n.T("Only remove these if no other git is running right now — a live git holding one is doing real work, and deleting its lock would corrupt that write."))

	paths := make([]string, 0, len(h.StaleLocks))
	for _, l := range h.StaleLocks {
		paths = append(paths, l.Path)
	}
	remove := i18n.T("Remove the lock files")
	if len(paths) == 1 {
		remove = i18n.T("Remove the lock file")
	}
	return &notice{
		id:      noticeStaleLock,
		repoKey: h.GitCommonDir,
		title:   title,
		detail:  detail,
		actions: []noticeAction{
			{label: remove, run: func(m Model) (Model, tea.Cmd) {
				m.refreshHealthAfterOp = true
				return m.startOp(engine.RemoveGitLocks{Paths: paths})
			}},
			{label: i18n.T("Not now (ask again next load)")},
			{label: i18n.T("Never for this repo"), never: true},
		},
	}
}

// maybeStaleLockNotice reacts to an operation that failed because a git
// lockfile is in the way. It re-reads repo health (which rescans for locks)
// so the stale-lock notice appears immediately, and points the user at the
// key that opens it — otherwise the failure reads as a dead end and the only
// apparent fix is deleting the file by hand, which is exactly the pain this
// feature removes.
//
// It also clears the notice's SESSION dismissal. Every other notice is an
// advisory where "Not now" rightly silences it until the next load; this one
// is a hard blocker, and a fresh failure is new information — not the same
// advice resurfacing. Without this, dismissing it once would leave the user
// stuck with an unfixable error for the rest of the session.
func (m Model) maybeStaleLockNotice(err error) Model {
	if err == nil || !domain.IsLockError(err) {
		return m
	}
	if m.noticeSessionDismissed != nil {
		delete(m.noticeSessionDismissed, noticeStaleLock)
	}
	m.refreshHealthAfterOp = true
	m.statusMsg = m.statusMsg + i18n.T("  — press [!] to remove the lock")
	return m
}

// lockAgeLabel renders how long a lock has been sitting there. Age is the only
// staleness signal available, so it is shown per lock rather than aggregated.
func lockAgeLabel(d time.Duration) string {
	switch {
	case d < time.Minute:
		return i18n.T("less than a minute old")
	case d < time.Hour:
		return i18n.T("%dm old", int(d.Minutes()))
	case d < 24*time.Hour:
		return i18n.T("%dh old", int(d.Hours()))
	default:
		return i18n.T("%dd old", int(d.Hours()/24))
	}
}

// startCommitGraphWriteAndEnable is THE write+enable code path, shared by the
// notice's first action and the Settings "Commit-graph" row: write the graph
// now, then (chained on success in opFinishedMsg) enable auto-refresh.
func (m Model) startCommitGraphWriteAndEnable() (Model, tea.Cmd) {
	m.pendingNoticeConfig = &engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}
	m.refreshHealthAfterOp = true
	return m.startOp(engine.WriteCommitGraph{})
}

// clipboardNotice fires when a local display session (X11 or Wayland) is
// present but no native clipboard tool is installed — the case where gg's copy
// actions silently fall back to an OSC 52 escape that many terminals (and tmux
// without extra config) do not honour. It is informational: gg cannot install
// a package for the user (needs root, distro-specific), so the only actions are
// the two dismissals. Installing the tool makes the notice self-clear on the
// next load (Probe then reports Available), so it never nags after it is fixed.
func clipboardNotice(av clipboard.Availability, repoKey string) *notice {
	if av.Available || av.Install == "" {
		return nil
	}
	if av.WSLInteropBroken {
		return nil // wslInteropNotice covers this same failure, with the real cause.
	}
	detail := []string{
		i18n.T("gg's copy actions (commit SHAs, branch/tag names, diffs) can't reach your system clipboard: a Linux terminal app needs a small helper program and none is installed."),
		i18n.T("gg is falling back to a terminal escape (OSC 52) that many terminals — and tmux without extra config — don't honour, so a copy can silently do nothing."),
		"",
		i18n.T("Install one, then copy again — gg picks it up automatically, no restart:"),
		"",
	}
	detail = append(detail, clipboardInstallLines(av.Install)...)
	return &notice{
		id:      noticeClipboard,
		repoKey: repoKey,
		title:   i18n.T("Clipboard copy may not work — install a clipboard tool"),
		detail:  detail,
		actions: []noticeAction{
			{label: i18n.T("Not now (ask again next load)")},
			{label: i18n.T("Never for this repo"), never: true},
		},
	}
}

// wslInteropNotice fires on WSL when the kernel cannot execute Windows
// binaries: clip.exe is on PATH and looks usable, but running it fails with
// "exec format error", so every copy falls through to an OSC 52 escape that
// tmux does not forward — and the status line still says "Copied …".
//
// It only fires when NOTHING else can reach the clipboard. If wl-copy or xclip
// covers it, copy works and gg stays quiet; broken Windows-exe interop in
// general is the user's business, not gg's. Dismiss-only, because the fix needs
// root; it self-clears on the next load once either remedy lands.
func wslInteropNotice(av clipboard.Availability, repoKey string) *notice {
	if !av.WSLInteropBroken || av.Available {
		return nil
	}
	detail := []string{
		i18n.T("Copying is broken on this machine: WSL cannot run Windows programs, so gg can't hand text to clip.exe."),
		i18n.T("gg falls back to a terminal escape (OSC 52) that tmux doesn't forward, so a copy silently does nothing — even though the status line says it worked."),
		"",
		i18n.T("The cause is outside gg: the kernel has no WSLInterop entry (systemd-binfmt drops it at boot when systemd is enabled in /etc/wsl.conf)."),
		"",
		i18n.T("Fix it once, so it survives a reboot:"),
		"",
		"    sudo sh -c 'mkdir -p /etc/binfmt.d && \\",
		"      echo \":WSLInterop:M::MZ::/init:PF\" > /etc/binfmt.d/WSLInterop.conf'",
		"    sudo systemctl restart systemd-binfmt",
		"",
		i18n.T("Or, without touching binfmt — install a Linux clipboard tool, which gg will use instead (WSLg shares its clipboard with Windows):"),
		"",
	}
	detail = append(detail, clipboardInstallLines("wl-clipboard")...)
	return &notice{
		id:      noticeWSLInterop,
		repoKey: repoKey,
		title:   i18n.T("Clipboard copy is broken — WSL can't run Windows programs"),
		detail:  detail,
		actions: []noticeAction{
			{label: i18n.T("Not now (ask again next load)")},
			{label: i18n.T("Never for this repo"), never: true},
		},
	}
}

// clipboardInstallLines renders per-distro install commands for the suggested
// package (xclip for X11, wl-clipboard for Wayland).
func clipboardInstallLines(pkg string) []string {
	return []string{
		i18n.T("    Debian/Ubuntu:  sudo apt install %s", pkg),
		i18n.T("    Fedora:         sudo dnf install %s", pkg),
		i18n.T("    Arch:           sudo pacman -S %s", pkg),
	}
}

// removeNotice drops one notice from the session list. A drift notice's
// source is dropped too (matched by id, cheap — driftNoticeID needs no
// translation), or the next rebuildNotices (a health re-read, a language
// switch) would re-derive and resurrect the one just acted on/dismissed.
func (m Model) removeNotice(id string) Model {
	var next []notice
	for _, n := range m.notices {
		if n.id != id {
			next = append(next, n)
		}
	}
	m.notices = next
	var nextDrift []driftNoticeSource
	for _, d := range m.driftNotices {
		if driftNoticeID(d.branch, d.report) != id {
			nextDrift = append(nextDrift, d)
		}
	}
	m.driftNotices = nextDrift
	return m
}

// driftNoticeSource is one post-op drift/paused-resume finding kept across
// notice rebuilds (a repoHealth re-read, a language switch): rebuildNotices
// re-renders these every time, exactly like it re-derives every
// repoHealth-based notice, rather than a rendered notice having to survive
// m.notices being wholesale reassigned by every other rebuild trigger.
type driftNoticeSource struct {
	branch string
	report domain.DriftReport
	paused bool
}

// driftNoticeID is driftNotice's id, factored out so removeNotice can match
// a drift notice's source without re-rendering (and thus without needing the
// active language) — the id is built from the version ref alone, which is
// language-independent.
func driftNoticeID(branch string, report domain.DriftReport) string {
	if report.Ref != "" {
		return noticeBranchDriftPrefix + report.Ref
	}
	// No ref to key on (Checked was false; only paused forced this to be
	// rendered at all) — fall back to the branch name. Rare: ContinueOp only
	// arms the drift check when a paused merge/rebase was in progress, which
	// implies the pre-op SmartRebase/SmartMerge/SmartPull already recorded a
	// version for this branch, so DriftAfter should normally find one.
	return noticeBranchDriftPrefix + branch
}

// driftNotice renders the post-op notice for one finding: branch's newest
// recorded version no longer matches what it contributes now (a file
// upstream deleted came back — see internal/changeset's doc comment), or the
// operation that produced it paused for conflicts before completing (the
// TUI's own resume path via engine.ContinueOp — the CLI has no such lane and
// so can only ever report the drifted case). Either is grounds to raise
// this; nil when neither applies. A pure function of already-fetched data
// (the commitGraphNotice/staleLockNotice precedent) so rebuildNotices can
// re-derive it on every rebuild instead of a raw notice having to survive
// m.notices being reassigned wholesale.
func driftNotice(branch string, report domain.DriftReport, paused bool, repoKey string) *notice {
	drifted := report.Checked && report.Report.Drifted()
	if !drifted && !paused {
		return nil
	}
	var title string
	var detail []string
	if drifted {
		title = i18n.T("%s's change set may have drifted from its recorded version", branch)
		detail = append(detail, i18n.T("%s now introduces changes its recorded version did not:", branch))
		for _, e := range report.Report.Added {
			// Raw path/status data, like staleLockNotice's per-lock lines
			// above — not translatable prose, so no i18n.T call here.
			detail = append(detail, "  "+string(rune(e.Status))+" "+e.Path)
		}
		if paused {
			detail = append(detail, i18n.T("The operation also paused for conflicts before completing."))
		}
	} else {
		title = i18n.T("%s paused for conflicts before completing", branch)
		detail = append(detail, i18n.T("%s paused for conflicts before this operation completed.", branch))
		if report.Checked {
			// Only true when a comparison actually ran (report.Checked): a
			// fast-forward pull, or a branch with nothing recorded to compare
			// against, never reached the diff at all — saying it "still
			// matches" would claim a comparison that never happened.
			detail = append(detail, i18n.T("Its change set still matches the recorded version, but the resolution is worth a look."))
		} else {
			detail = append(detail, i18n.T("Nothing was recorded to compare it against, but the resolution is worth a look."))
		}
	}
	detail = append(detail, i18n.T("Open Branch versions to compare it against what gg recorded before this operation."))
	return &notice{
		id:      driftNoticeID(branch, report),
		repoKey: repoKey,
		title:   title,
		detail:  detail,
		actions: []noticeAction{
			{label: i18n.T("Dismiss")},
		},
	}
}

// driftArmFor decides the post-op DriftAfter check startOp should arm for
// op, if any: which branch to check, and whether this dispatch resumes a
// merge/rebase that had paused for conflicts (the spec's other trigger,
// alongside drift itself). currentBranch is m.status.Branch at dispatch
// time (the "" default for a rung-1 rebase/merge/pull, which act on the
// branch checked out here); conflict is m.conflict, still reflecting the
// PAUSED state at this instant for a ContinueOp dispatch — the resume runs
// before any status re-read. A pure function of the dispatch, with no I/O,
// so it is testable without running the op through a real service.
//
// Cherry-pick/revert never record a two-branch version (snapshot_version.go
// only calls snapshotBranchTip[Named] from the merge/rebase/pull/interactive-
// rebase paths), so a ContinueOp resuming one of those arms nothing.
func driftArmFor(op engine.Operation, currentBranch string, conflict domain.ConflictState) (branch string, paused bool) {
	switch v := op.(type) {
	case engine.SmartRebase:
		branch = v.Branch
		if branch == "" {
			branch = currentBranch // rung 1: rebase in place, branch defaults to current
		}
	case engine.SmartMerge:
		branch = v.Target
		if branch == "" {
			branch = currentBranch // merge target defaults to current
		}
	case engine.SmartPull:
		branch = v.Branch
		if branch == "" {
			branch = currentBranch
		}
	case engine.InteractiveRebase:
		branch = v.Branch // always explicit: Run refuses an empty Branch
	case engine.ContinueOp:
		// Attribute the branch the same way domain.conflictState does:
		// rebase snapshots the rebased branch itself (Source), merge
		// snapshots the branch merged INTO (Target).
		switch conflict.Op {
		case "merge":
			branch, paused = conflict.Target, true
		case "rebase":
			branch, paused = conflict.Source, true
		}
	}
	return branch, paused
}

// driftCheckMsg carries a post-op DriftAfter comparison for one branch. gen
// guards a repo switch (reRoot bumps noticeGen the same way it does for
// repoHealthMsg): a check dispatched for the OLD repo must never surface as
// a notice for the NEW one.
type driftCheckMsg struct {
	gen    int
	branch string
	paused bool
	report domain.DriftReport
	err    error
}

// driftCheckCmd runs DriftAfter off the UI thread for branch. Dispatched
// from opFinishedMsg once a rebase/merge/pull (or its resume via
// engine.ContinueOp) finishes successfully — armed earlier by startOp's type
// switch (m.pendingDriftBranch/m.pendingDriftPaused), per the spec's rule:
// detection runs AFTER Execute releases the gate, never inside it.
func (m Model) driftCheckCmd(branch string, paused bool, gen int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		rep, err := svc.DriftAfter(context.Background(), branch)
		return driftCheckMsg{gen: gen, branch: branch, paused: paused, report: rep, err: err}
	}
}

// applyDriftReport is the driftCheckMsg success handler: renders the
// finding, and — only if it is actually worth saying anything (driftNotice
// returned non-nil) — records its source and re-blinks, the same "genuinely
// new" gate applyRepoHealth uses (simplified: an appended driftNoticeSource
// is always new this session, so no prev/next id diff is needed here).
//
// Deduped by driftNoticeID (branch + version ref) before appending: two
// findings sharing one ref — e.g. a paused rebase raises on ref V, the
// resume raises on V again, and re-checking the same branch before any
// further recording op still reports V — must REPLACE the existing source,
// never add a second. (Every pull now records its own version, so a later
// pull is NOT an example of this; it moves the branch to a fresh ref.)
// Left unchecked, m.notices would carry two rows with the same id,
// and removeNotice (which matches by id) would drop both at once on a
// single Dismiss.
func (m Model) applyDriftReport(branch string, report domain.DriftReport, paused bool) (Model, tea.Cmd) {
	if driftNotice(branch, report, paused, m.repoHealth.GitCommonDir) == nil {
		return m, nil // no drift, and the op didn't pause for conflicts: nothing to say
	}
	id := driftNoticeID(branch, report)
	next := make([]driftNoticeSource, 0, len(m.driftNotices)+1)
	for _, d := range m.driftNotices {
		if driftNoticeID(d.branch, d.report) != id {
			next = append(next, d)
		}
	}
	m.driftNotices = append(next, driftNoticeSource{branch: branch, report: report, paused: paused})
	m = m.rebuildNotices()
	var cmd tea.Cmd
	if !m.noticesUnread {
		m.blinkGen++
		cmd = noticeBlinkCmd(m.blinkGen)
	}
	m.noticesUnread = true
	m.blinkOn = true
	return m, cmd
}

// noticeSegment renders the status-bar segment: red + phase-alternating while
// unread, calm plain text once read, "" when there is nothing to say (or the
// conflict process owns the screen).
func (m Model) noticeSegment() string {
	n := len(m.notices)
	if n == 0 || m.proc != nil {
		return ""
	}
	seg := i18n.T("! %d notice — press [!]", n)
	if n != 1 {
		seg = i18n.T("! %d notices — press [!]", n)
	}
	if !m.noticesUnread {
		return seg
	}
	if m.blinkOn {
		return st().noticeHot.Render(seg)
	}
	return st().noticeDim.Render(seg)
}
