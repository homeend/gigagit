package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/clipboard"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
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

// bigRepoPackBytes is the pack-size floor for "big repo": below it the
// commit-graph win doesn't matter enough to nag about.
const bigRepoPackBytes = 100 << 20

// Blink = style alternation between these two on a dedicated tick;
// terminal-native blink escapes are unreliable.
var (
	noticeHotStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	noticeDimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("124"))
)

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

// rebuildNotices re-derives m.notices from the cached health snapshot.
// Notice titles/details/action labels bake i18n.T output at build time, so a
// language switch must rebuild them; ids are stable, so dismissals hold and
// no blink logic runs here (applyRepoHealth owns blinking).
func (m Model) rebuildNotices() Model {
	if !m.repoHealthKnown {
		return m // nothing cached yet: the first health read builds in the new language
	}
	var dismissed map[string]bool
	if m.promptStore != nil {
		dismissed = m.promptStore.DismissedNotices(m.repoHealth.GitCommonDir)
	}
	var next []notice
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
	m.notices = next
	return m
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

// removeNotice drops one notice from the session list.
func (m Model) removeNotice(id string) Model {
	var next []notice
	for _, n := range m.notices {
		if n.id != id {
			next = append(next, n)
		}
	}
	m.notices = next
	return m
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
		return noticeHotStyle.Render(seg)
	}
	return noticeDimStyle.Render(seg)
}
