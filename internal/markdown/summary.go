package markdown

import "strings"

// Summary labels: what a thread is called when its body opens with a block
// that has no first line of prose. English protocol text; a frontend may
// translate them at render.
const (
	LabelCode       = "code"
	LabelSuggestion = "suggestion"
	LabelQuote      = "quote"
	LabelTable      = "table"
	LabelRule       = "—"
)

// Summary splits a review comment into the one-line summary a note box shows
// bold and the rest, its rationale (still markdown).
//
//   - A body opening with prose or a heading: the summary is that first line
//     with its markers stripped, the rest is everything after it — for plain
//     prose exactly the first-line split gg always made.
//   - A body opening with a list: the summary is the first item's first line,
//     and the rest is the WHOLE body, so the list stays a list.
//   - A body opening with a fence, a quote, a table or a rule: the summary is
//     a label and the rest is the whole body.
//
// rawFirst is the summary line with its markers intact (empty for a label),
// for a frontend that renders the summary with inline styling.
func Summary(src string) (summary, rest, rawFirst string) {
	body := strings.TrimSpace(normalise(src))
	if body == "" {
		return "", "", ""
	}
	first, after, _ := strings.Cut(body, "\n")
	if len(body) > MaxInput {
		return strings.TrimSpace(first), strings.TrimSpace(after), ""
	}
	if f, ok := fenceOpen(first); ok {
		if strings.EqualFold(fenceLang(f.info), LangSuggestion) {
			return LabelSuggestion, body, ""
		}
		return LabelCode, body, ""
	}
	if isRule(first) {
		return LabelRule, body, ""
	}
	if isQuote(first) {
		return LabelQuote, body, ""
	}
	if _, text, ok := atxHeading(first); ok {
		return plainOr(text, first), strings.TrimSpace(after), text
	}
	if m, ok := listMarker(first); ok && !m.empty {
		_, text := taskMarker(first[m.content:])
		return plainOr(text, first), body, text
	}
	if strings.Contains(first, "|") {
		lines := strings.SplitN(body, "\n", 3)
		if len(lines) > 1 {
			if _, _, ok := parseTable(lines[:2], 0); ok {
				return LabelTable, body, ""
			}
		}
	}
	first = strings.TrimSpace(first)
	return plainOr(first, first), strings.TrimSpace(after), first
}

// plainOr is PlainInline(text), or fallback's trimmed text when the line is
// all markers (an image with no alt, say).
func plainOr(text, fallback string) string {
	if p := PlainInline(text); p != "" {
		return p
	}
	return strings.TrimSpace(fallback)
}

// PlainInline is one line of inline markdown as plain text: markers dropped,
// a link reduced to its text, an image to its alt text, runs of whitespace
// folded.
func PlainInline(s string) string {
	return strings.Join(strings.Fields(flatText(ParseInline(s))), " ")
}
