package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// commitForMessageView resolves the real commit under the Commits-panel cursor
// for the `i`/`e` message actions. It is wip-safe: backingIndex(panelCommits)
// returns ok=false on a ◇ WIP pseudo-row, so those drop out. Returns ok=false
// when the Commits panel isn't focused, an op is running, or no commit is
// selected.
func (m Model) commitForMessageView() (model.Commit, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return model.Commit{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return model.Commit{}, false
	}
	return m.commits[bi], true
}

// commitMessageTitle is the contentPopup title for a commit-message view. It
// doubles as the async tag: the commitMessageMsg handler fills only the popup
// whose title byte-matches, so a stale load from a different commit (the user
// esc'd and reopened on another row) can't land in the wrong popup.
func commitMessageTitle(short string) string { return i18n.T("Commit %s message", short) }

// commitMessageMsg carries the async result of loadCommitMessageCmd: the full
// popup content (the git-show-style metadata header built from the in-memory
// commit, then the fetched message body) for the contentPopup pushed by the `i`
// action. Tagged by short hash to gate stale loads.
type commitMessageMsg struct {
	short string
	lines []contentLine
}

// openCommitMessagePopup pushes a loading contentPopup for c's full message and
// kicks off the async body fetch. The metadata header + footer are built from
// the already-loaded commit (no git call); only the message body is async. The
// popup scrolls/pages/searches like any other.
func (m Model) openCommitMessagePopup(c model.Commit) (Model, tea.Cmd) {
	short := shortHash(c.Hash)
	cp := newContentPopup(commitMessageTitle(short), append(commitMetaHeader(c), contentLine{text: i18n.T("(loading…)")}))
	cp.footer = commitFooterLine(c)
	m = m.pushLayer(cp)
	return m, m.loadCommitMessageCmd(c, short)
}

// loadCommitMessageCmd reads the full commit message off the UI thread and
// delivers commitMessageMsg with the metadata header followed by the body. The
// trailing newline of `git log -1 --pretty=%B` is trimmed so the popup has no
// blank last row; a read failure is shown in place of the body (header kept).
func (m Model) loadCommitMessageCmd(c model.Commit, short string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		lines := commitMetaHeader(c)
		msg, err := svc.CommitMessage(context.Background(), c.Hash)
		if err != nil {
			lines = append(lines, contentLine{text: i18n.T("(load failed: %s)", err.Error())})
		} else {
			lines = append(lines, fileContentLines([]byte(strings.TrimRight(msg, "\n")))...)
		}
		return commitMessageMsg{short: short, lines: lines}
	}
}

// commitMetaHeader builds the git-show-style header block from a commit's
// already-loaded fields: full hash, author, date, ref decorations (when any),
// and merge parents (when a merge), followed by a blank separator before the
// body. No git call — every field is on model.Commit.
func commitMetaHeader(c model.Commit) []contentLine {
	out := []contentLine{
		{text: i18n.T("commit %s", c.Hash)},
		{text: i18n.T("Author: %s", c.Author)},
		{text: i18n.T("Date:   %s", commitDateString(c))},
	}
	if refs := commitRefsLine(c); refs != "" {
		out = append(out, contentLine{text: i18n.T("Refs:   %s", refs)})
	}
	if len(c.Parents) > 1 {
		shorts := make([]string, len(c.Parents))
		for i, p := range c.Parents {
			shorts[i] = shortHash(p)
		}
		out = append(out, contentLine{text: i18n.T("Merge:  %s", strings.Join(shorts, " "))})
	}
	return append(out, contentLine{text: ""})
}

// commitFooterLine is the compact author · date echo shown on the popup's footer
// line (above the hint).
func commitFooterLine(c model.Commit) string {
	if c.Author == "" {
		return commitDateString(c)
	}
	return c.Author + " · " + commitDateString(c)
}

// commitDateString formats a commit's author time as a local absolute stamp.
func commitDateString(c model.Commit) string {
	if c.UnixTime == 0 {
		return i18n.T("(unknown)")
	}
	return time.Unix(c.UnixTime, 0).Format("2006-01-02 15:04")
}

// commitRefsLine joins a commit's ref decorations (tags prefixed "tag: ") for
// the header; "" when undecorated.
func commitRefsLine(c model.Commit) string {
	if len(c.Refs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(c.Refs))
	for _, r := range c.Refs {
		if r.Kind == model.RefTag {
			parts = append(parts, i18n.T("tag: %s", r.Name))
		} else {
			parts = append(parts, r.Name)
		}
	}
	return strings.Join(parts, ", ")
}

// openCommitMessageEditor opens c's full message in $EDITOR (read-only temp,
// via the editorView msg pair that skips the working-tree status reload). The
// bytes are left untrimmed — the editor shows the message as git stores it; the
// COMMIT_EDITMSG name nudges editors into commit-message highlighting.
func (m Model) openCommitMessageEditor(c model.Commit) (Model, tea.Cmd) {
	svc, hash := m.svc, c.Hash
	return m, m.openInEditorCmd("COMMIT_EDITMSG", func(ctx context.Context) ([]byte, error) {
		s, err := svc.CommitMessage(ctx, hash)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	})
}

// commitViewMessageRow / commitEditMessageRow surface the `i`/`e` actions in the
// Commits-panel `.` menu (self-gating, same resolver as the keys). Both carry a
// direct run handler so they're safe even on the files-view commit-list side.
func (m Model) commitViewMessageRow() (actionRow, bool) {
	c, ok := m.commitForMessageView()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commit-view-message",
		label: i18n.T("View message"),
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.openCommitMessagePopup(c) },
	}, true
}

func (m Model) commitEditMessageRow() (actionRow, bool) {
	c, ok := m.commitForMessageView()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commit-edit-message",
		label: i18n.T("Open message in editor"),
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.openCommitMessageEditor(c) },
	}, true
}
