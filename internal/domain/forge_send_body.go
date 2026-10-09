package domain

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

// sendBody is what gg posts for one note (spec §3.6): an optional quote
// (a file-level thread standing in for a line outside the diff), the
// summary, the rationale — folded into <details> with the tags and the
// confidence when an agent wrote it, which also signs it — and the marker.
func sendBody(n model.Note, key, quote string) string {
	var b strings.Builder
	b.WriteString(quote)
	b.WriteString(strings.TrimSpace(n.Summary))
	agent := n.Source == model.NoteSourceAgent
	rat := strings.TrimSpace(n.Rationale)
	switch {
	case agent && (rat != "" || len(n.Tags) > 0 || n.Confidence > 0):
		b.WriteString("\n\n<details><summary>Details</summary>\n\n")
		if rat != "" {
			b.WriteString(rat + "\n\n")
		}
		for _, t := range n.Tags {
			b.WriteString("- " + t + "\n")
		}
		if n.Confidence > 0 {
			b.WriteString("\nconfidence: " + strconv.FormatFloat(n.Confidence, 'g', -1, 64) + "\n")
		}
		b.WriteString("\n</details>")
	case rat != "":
		b.WriteString("\n\n" + rat)
	}
	if agent {
		who := strings.TrimSpace(n.Author)
		if who == "" {
			who = "agent"
		}
		b.WriteString("\n\n— " + who + " via gg")
	}
	b.WriteString("\n\n" + forge.SendMarker(key))
	return b.String()
}

// quoteLines opens a file-level thread with the lines it is really about.
// The fence outgrows any backtick run in them, so a quoted ``` cannot
// close it.
func quoteLines(line int, text []string) string {
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

// reviewSendBody is a whole review's GitHub body: its overview (or its
// prose), signed by its agent, marked with the review note's id.
func reviewSendBody(r Review) string {
	text := r.Text
	if r.Doc != nil {
		text = r.Doc.Summary
	}
	n := model.Note{Source: model.NoteSourceAgent, Author: r.Agent, Summary: strings.TrimSpace(text)}
	return sendBody(n, r.ID, "")
}
