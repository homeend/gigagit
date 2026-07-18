package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// repoPathPopup takes a filesystem path to a git repository (any path inside one)
// and switches gg to it. Reached from the command palette's "Open repo". The path
// is validated + normalized off the UI thread via TopLevel before the switch: a
// non-repo path shows an inline error and keeps the popup open (no half-switch on
// a typo). Distinct from the MRU repoPopup (R), which only lists previously
// opened repos.
type repoPathPopup struct {
	popupMax
	input     textfield
	err       string // inline error from the last failed validation; "" = none
	resolving bool   // a validation cmd is in flight
}

func (m Model) openRepoPathPopup() (Model, tea.Cmd) {
	return m.pushLayer(&repoPathPopup{input: newTextField("")}), nil
}

// repoResolvedMsg carries the result of validating a typed repo path. path is the
// exact text submitted (the tag-gate key, before ~ expansion); top is the
// resolved repo root on success; err is non-nil when the path is not in a repo.
type repoResolvedMsg struct {
	path string
	top  string
	err  error
}

// expandHome expands a leading ~ or ~/ to the user's home dir. os.UserHomeDir
// (not $HOME) is used so it works on Windows too.
func expandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func (m Model) resolveRepoCmd(raw string) tea.Cmd {
	return func() tea.Msg {
		svc := domain.OpenTUI(expandHome(raw))
		top, err := svc.TopLevel(context.Background())
		return repoResolvedMsg{path: raw, top: top, err: err}
	}
}

func (p *repoPathPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		raw := strings.TrimSpace(p.input.Value())
		if raw == "" { // nothing to resolve; keep the popup open
			return m, nil
		}
		p.resolving = true
		p.err = ""
		return m, m.resolveRepoCmd(raw)
	default:
		if p.input.HandleEditKey(msg) { // spaces included — do NOT swallow KeySpace
			p.err = "" // editing clears the stale error
		}
	}
	return m, nil
}

// resolvedRepoPath applies a validation result. Tag-gated by the caller: acts
// only when this popup is on top and its input still equals msg.path.
func (m Model) resolvedRepoPath(p *repoPathPopup, msg repoResolvedMsg) (tea.Model, tea.Cmd) {
	p.resolving = false
	if msg.err != nil {
		p.err = i18n.T("not a git repository: %s", msg.path)
		return m, nil
	}
	m = m.popLayer() // the repo popup
	if _, ok := m.topLayer().(*commandPalette); ok {
		m = m.popLayer() // the palette that launched it
	}
	return m.reRoot(msg.top)
}

func (p *repoPathPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *repoPathPopup) box(m Model) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString(i18n.T("Open repo") + "\n\n")
	b.WriteString(viewField(i18n.T("path: "), p.input, true, popupContentWidth(w)) + "\n")
	if p.err != "" {
		b.WriteString("\n" + errorStyle.Render(p.err) + "\n")
	}
	b.WriteString("\n" + i18n.T("[enter] open  [esc] cancel"))
	return modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
