package agentdocs

import (
	"regexp"

	"github.com/homeend/gigagit/internal/markdown"
)

// BacktickedAnchor is an anchor written inside a code span
// ("`[label](path:N)`"): every renderer shows it as text, so it opens
// nothing. Link is the link without the backticks — what to write.
type BacktickedAnchor struct{ Link string }

// Problem is how a save reports it: what is wrong and what to write.
func (b BacktickedAnchor) Problem() string {
	return "`" + b.Link + "`: a link inside backticks is plain text, not an anchor — write " +
		b.Link + " (for a code look, put the backticks inside the brackets)"
}

var linkInCode = regexp.MustCompile(`^\s*\[([^\]\n]*)\]\(([^)\s]+)\)\s*$`)

// BacktickedAnchors lists, in document order, every code span of text that
// is one whole link to an anchor destination (IsAnchorDest) — what an agent
// writes when it copies an example's backticks. Code that merely contains
// brackets (`arr[i](x)`), a web link and a fenced block (an example meant
// as text) are not reported.
func BacktickedAnchors(text string) []BacktickedAnchor {
	var out []BacktickedAnchor
	var inl func([]markdown.Inline)
	inl = func(in []markdown.Inline) {
		for _, n := range in {
			if n.Kind == markdown.InCode {
				if m := linkInCode.FindStringSubmatch(n.Text); m != nil && IsAnchorDest(m[2]) {
					out = append(out, BacktickedAnchor{Link: "[" + m[1] + "](" + m[2] + ")"})
				}
				continue
			}
			inl(n.In)
		}
	}
	var blocks func([]markdown.Block)
	blocks = func(bs []markdown.Block) {
		for _, b := range bs {
			inl(b.Inline)
			blocks(b.Blocks)
			for _, it := range b.Items {
				blocks(it.Blocks)
			}
			for _, c := range b.Head {
				inl(c)
			}
			for _, r := range b.Rows {
				for _, c := range r {
					inl(c)
				}
			}
		}
	}
	blocks(markdown.Parse(text).Blocks)
	return out
}
