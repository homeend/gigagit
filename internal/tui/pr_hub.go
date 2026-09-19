package tui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// prHubPopup is the pull-request hub: title, state line, description, the
// conversation with review verdicts, and the outdated inline threads. It is a
// contentPopup (scroll, / filter, s save, ctrl+t maximize, esc back to the
// opener) plus two keys of its own: y copies the PR URL, r reloads. Read-only,
// like everything forge.
type prHubPopup struct {
	*contentPopup
	pr model.PullRequest
}

// prHubMsg is the hub's async load: the PR re-read WITH its body (the list
// read carries none) and its comments. number gates a stale load.
type prHubMsg struct {
	number int
	pr     model.PullRequest
	c      domain.PRComments
	err    error
}

// prHubHunkLines is how much of an outdated thread's hunk the hub shows: the
// tail, where the commented line is.
const prHubHunkLines = 6

func prHubTitle(n int) string { return i18n.T("Pull request #%d", n) }

// openPRHub pushes the hub with the header it already knows and loads the rest
// off the UI thread.
func (m Model) openPRHub(p model.PullRequest) (Model, tea.Cmd) {
	cp := newContentPopup(prHubTitle(p.Number), prHubLoading(p))
	cp.mode = modeWrap // descriptions and comments are prose
	cp.noCursor = true
	cp.footer = i18n.T("[y] copy URL  [r] reload")
	m = m.pushLayer(&prHubPopup{contentPopup: cp, pr: p})
	return m, m.loadPRHubCmd(p.Number)
}

func prHubLoading(p model.PullRequest) []contentLine {
	return append(prHubHeader(p, time.Now()), contentLine{text: i18n.T("(loading…)")})
}

func (m Model) loadPRHubCmd(n int) tea.Cmd {
	svc := m.svc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		out := prHubMsg{number: n}
		if out.pr, out.err = svc.PullRequest(ctx, n); out.err != nil {
			return out
		}
		out.c, out.err = svc.PRComments(ctx, n)
		return out
	}
}

// handlePRHubMsg fills the hub the load was started for; any other arrival
// (the hub closed, another PR's hub opened since) is dropped.
func (m Model) handlePRHubMsg(msg prHubMsg) (Model, tea.Cmd) {
	hub := layerOf[*prHubPopup](m)
	if hub == nil || hub.pr.Number != msg.number {
		return m, nil
	}
	now := time.Now()
	if msg.err != nil {
		hub.lines = append(prHubHeader(hub.pr, now),
			contentLine{text: i18n.T("(load failed: %s)", firstLine(msg.err.Error()))},
			contentLine{text: i18n.T("[r] retry")})
		hub.sel = 0
		return m, nil
	}
	if msg.pr.Number == hub.pr.Number {
		hub.pr = msg.pr
	}
	hub.lines = append(prHubHeader(hub.pr, now), prHubBody(hub.pr, msg.c, now)...)
	hub.sel = 0
	return m, nil
}

func (p *prHubPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if !p.typing { // while the / filter is capturing, every key is query text
		switch msg.String() {
		case "y":
			if p.pr.URL == "" {
				m.statusMsg = i18n.T("this pull request has no URL")
				return m, nil
			}
			return m, m.copyToClipboardCmd(i18n.T("copied PR URL"), p.pr.URL)
		case "r":
			p.lines, p.sel, p.query = prHubLoading(p.pr), 0, ""
			return m, m.loadPRHubCmd(p.pr.Number)
		}
	}
	return p.contentPopup.update(m, msg)
}

// prVerdictWord is a review's verdict as the hub spells it ("" = no verdict).
func prVerdictWord(v string) string {
	switch strings.ToLower(v) {
	case "approved":
		return i18n.T("approved")
	case "changes_requested":
		return i18n.T("changes requested")
	case "commented":
		return i18n.T("commented")
	}
	return ""
}

// prReviewStateWord is the PR-level review state of the header line.
func prReviewStateWord(s string) string {
	switch s {
	case "APPROVED":
		return i18n.T("approved")
	case "CHANGES_REQUESTED":
		return i18n.T("changes requested")
	case "REVIEW_REQUIRED":
		return i18n.T("review required")
	}
	return ""
}

// prHubHeader is "#N title", the state line and the URL, then a blank.
func prHubHeader(p model.PullRequest, now time.Time) []contentLine {
	state := prStateWord(p.State)
	if p.IsOpen() {
		state = i18n.T("open")
	}
	parts := []string{state}
	if p.Draft && p.IsOpen() {
		parts = append(parts, i18n.T("draft"))
	}
	for _, s := range []string{p.Author, prBranches(p)} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if w := prReviewStateWord(p.ReviewState); w != "" && p.IsOpen() {
		parts = append(parts, w)
	}
	if !p.Updated.IsZero() {
		parts = append(parts, i18n.T("updated %s", ageString(now, p.Updated)))
	}
	out := []contentLine{
		{text: "#" + strconv.Itoa(p.Number) + " " + sanitizeRowText(p.Title)},
		{text: strings.Join(parts, " · ")},
	}
	if p.URL != "" {
		out = append(out, contentLine{text: p.URL})
	}
	return append(out, contentLine{text: ""})
}

// prTextLines is forge prose as indented content lines: CRs stripped, tabs
// expanded and control characters neutralised by the diff view's sanitizer.
func prTextLines(body, indent string) []contentLine {
	body = strings.TrimRight(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var out []contentLine
	for _, l := range strings.Split(body, "\n") {
		out = append(out, contentLine{text: indent + sanitizeLine(l)})
	}
	return out
}

// prCommentHead is "author · age [· extra…]".
func prCommentHead(c model.ForgeComment, now time.Time, extra ...string) string {
	parts := []string{c.Author}
	if !c.Created.IsZero() {
		parts = append(parts, ageString(now, c.Created))
	}
	for _, e := range extra {
		if e != "" {
			parts = append(parts, e)
		}
	}
	return strings.Join(parts, " · ")
}

// prHubBody is everything under the header: Description, Conversation, and —
// only when there are any — the Outdated threads with their hunk tails.
func prHubBody(p model.PullRequest, c domain.PRComments, now time.Time) []contentLine {
	out := []contentLine{{text: i18n.T("Description"), heading: true}}
	if strings.TrimSpace(p.Body) == "" {
		out = append(out, contentLine{text: "  " + i18n.T("(no description)")})
	} else {
		out = append(out, prTextLines(p.Body, "  ")...)
	}
	out = append(out, contentLine{text: ""},
		contentLine{text: i18n.T("Conversation (%d)", len(c.Hub)), heading: true})
	if len(c.Hub) == 0 {
		out = append(out, contentLine{text: "  " + i18n.T("(no conversation yet)")})
	}
	for _, cm := range c.Hub {
		out = append(out, contentLine{text: "  " + prCommentHead(cm, now, prVerdictWord(cm.Verdict))})
		if strings.TrimSpace(cm.Body) != "" {
			out = append(out, prTextLines(cm.Body, "    ")...)
		}
	}
	if roots := countRoots(c.Outdated); roots > 0 {
		out = append(out, contentLine{text: ""},
			contentLine{text: i18n.T("Outdated (%d)", roots), heading: true})
		for _, cm := range c.Outdated {
			if cm.ParentID != "" { // a reply hangs under its thread's root
				out = append(out, contentLine{text: "    ↳ " + prCommentHead(cm, now)})
				out = append(out, prTextLines(cm.Body, "      ")...)
				continue
			}
			where := cm.Path
			if cm.Line > 0 {
				where += ":" + strconv.Itoa(cm.Line)
			}
			resolved := ""
			if cm.Resolved {
				resolved = i18n.T("resolved")
			}
			head := cm
			head.Author = where + " · " + cm.Author
			out = append(out, contentLine{text: "  " + prCommentHead(head, now, resolved)})
			if cm.Hunk != "" {
				out = append(out, prTextLines(strings.Join(tailLines(cm.Hunk, prHubHunkLines), "\n"), "    ")...)
			}
			out = append(out, prTextLines(cm.Body, "    ")...)
		}
	}
	if c.Truncated {
		out = append(out, contentLine{text: ""},
			contentLine{text: i18n.T("(comments truncated at the forge's page size)")})
	}
	return out
}

func countRoots(cs []model.ForgeComment) int {
	n := 0
	for _, c := range cs {
		if c.ParentID == "" {
			n++
		}
	}
	return n
}

// tailLines is the last n lines of s.
func tailLines(s string, n int) []string {
	ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return ls
}
