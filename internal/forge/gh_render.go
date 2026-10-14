package forge

import (
	"fmt"
	"regexp"
	"strings"
)

// GitHubRenderer is GitHub's markup (spec §8.2): the quote, the badge line
// (in an alert when the level asks), the summary, <details> with the
// rationale and the line entries, the signature, the marker.
type GitHubRenderer struct {
	// NoAlerts draws the plain bold badge line at every level: the fallback
	// if GitHub does not render alerts inside review comments.
	NoAlerts bool
}

func (GitHubRenderer) Capabilities() Capabilities { return Capabilities{Level: true} }

func (g GitHubRenderer) RenderComment(c Comment) string {
	var b strings.Builder
	if len(c.Quote) > 0 {
		b.WriteString(ghQuote(c.QuoteLine, c.Quote))
	}
	if bl := g.badgeBlock(c); bl != "" {
		b.WriteString(bl + "\n\n")
	}
	b.WriteString(strings.TrimSpace(c.Summary))
	agent := c.Author != ""
	rat := strings.TrimSpace(c.Rationale)
	var fold []string
	if rat != "" {
		if agent {
			fold = append(fold, rat)
		} else {
			b.WriteString("\n\n" + rat)
		}
	}
	if len(c.Lines) > 0 {
		var ls []string
		for _, l := range c.Lines {
			ls = append(ls, "- "+mdEscape(l.Label)+": "+mdEscape(l.Value))
		}
		fold = append(fold, strings.Join(ls, "\n"))
	}
	if len(fold) > 0 {
		b.WriteString("\n\n<details><summary>Details</summary>\n\n" + strings.Join(fold, "\n\n") + "\n\n</details>")
	}
	if agent {
		b.WriteString("\n\n— " + c.Author + " via gg")
	}
	b.WriteString("\n\n" + SendMarker(c.MarkerKey))
	return b.String()
}

func (GitHubRenderer) RenderReview(r ReviewBody) string {
	return GitHubRenderer{}.RenderComment(Comment{Summary: r.Summary, Author: r.Author, MarkerKey: r.MarkerKey})
}

func (GitHubRenderer) Unrender(body string) string {
	rest := body
	if n := ghQuoteLen(rest); n > 0 {
		rest = rest[n:]
	}
	if bl := ghBadgeRe.FindString(rest); bl != "" {
		return body[:len(body)-len(rest)] + rest[len(bl):]
	}
	return body
}

// ghBadgeRe is the badge block, in an alert or plain (either form: a body
// may predate a NoAlerts flip). A badge word holds no bare '*' (mdEscape),
// so "**a** and **b**" is not one.
var ghBadgeRe = regexp.MustCompile(`^(?:> \[!(?:CAUTION|WARNING|NOTE)\]\n> )?\*\*(?:\\.|[^*\\\n])+\*\*\n\n`)

// ghQuoteLen is the length of the quote ghQuote wrote at body's head, 0
// when it does not open with one.
func ghQuoteLen(body string) int {
	first, rest, ok := strings.Cut(body, "\n")
	if !ok || !ghQuoteHead.MatchString(first) {
		return 0
	}
	open, rest, ok := strings.Cut(rest, "\n")
	fence := strings.TrimPrefix(open, "> ")
	if !ok || len(fence) < 3 || strings.Trim(fence, "`") != "" {
		return 0
	}
	end := strings.Index(rest, "\n> "+fence+"\n\n")
	if end < 0 {
		return 0
	}
	return len(first) + 1 + len(open) + 1 + end + len("\n> "+fence+"\n\n")
}

var ghQuoteHead = regexp.MustCompile(`^> Line \d+:$`)

// badgeBlock is the badge words in bold, in the level's alert; "" without
// badges.
func (g GitHubRenderer) badgeBlock(c Comment) string {
	if len(c.Badges) == 0 {
		return ""
	}
	words := make([]string, len(c.Badges))
	for i, bd := range c.Badges {
		words[i] = mdEscape(bd.Value)
	}
	line := "**" + strings.Join(words, " · ") + "**"
	if tag := g.alert(c.Level); tag != "" {
		return "> [!" + tag + "]\n> " + line
	}
	return line
}

// alert is the GitHub alert a level opens; "" = the plain line.
func (g GitHubRenderer) alert(level string) string {
	if g.NoAlerts {
		return ""
	}
	switch level {
	case "high":
		return "CAUTION"
	case "medium":
		return "WARNING"
	case "low":
		return "NOTE"
	}
	return ""
}

// mdEscape backslash-escapes the markdown and HTML punctuation an agent's
// key or value could use to break the body's structure.
func mdEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_[]<>~|", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ghQuote opens a file-level thread with the lines it is really about. The
// fence outgrows any backtick run in them, so a quoted ``` cannot close it.
func ghQuote(line int, text []string) string {
	run := 2
	for _, l := range text {
		n := 0
		for _, r := range l {
			if r == '`' {
				n++
				run = max(run, n)
			} else {
				n = 0
			}
		}
	}
	fence := strings.Repeat("`", run+1)
	var b strings.Builder
	fmt.Fprintf(&b, "> Line %d:\n> %s\n", line, fence)
	for _, l := range text {
		b.WriteString("> " + l + "\n")
	}
	b.WriteString("> " + fence + "\n\n")
	return b.String()
}
