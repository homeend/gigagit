package forge

import "github.com/homeend/gigagit/internal/notemeta"

// Comment is one note as gg hands it to a forge (spec 2026-10-12-note-meta
// §8.1): neutral parts, never text. Badges and Lines are the author's
// declared meta only — stored-only keys, meta added later and the
// declaration itself never leave gg.
type Comment struct {
	Quote     []string // the lines a file-level thread stands in for
	QuoteLine int
	Summary   string
	Rationale string
	Badges    []notemeta.Badge
	Lines     []notemeta.Line
	Level     string
	Audience  string
	Author    string // agent signature; "" for the user's own
	MarkerKey string
}

// ReviewBody is a whole review's body: its summary, signed by its agent.
type ReviewBody struct{ Summary, Author, MarkerKey string }

// Capabilities are the gg semantics a renderer reads (R7): the rest of a
// comment's meta is simply not drawn by it.
type Capabilities struct{ Level, Audience bool }

// Renderer turns neutral parts into one forge's posted text. Domain hands
// it parts; only a renderer knows the forge's markup.
type Renderer interface {
	RenderComment(Comment) string
	RenderReview(ReviewBody) string
	Capabilities() Capabilities
	// Unrender is a body this renderer wrote as gg's own reader shows it: the
	// badge block taken out, so the summary leads again (§8.3). It reads the
	// body alone — the local item is gone once its send landed.
	Unrender(body string) string
}

// RendererFor is p's renderer; a provider without one posts GitHub's markup.
func RendererFor(p Provider) Renderer {
	if r, ok := p.(Renderer); ok {
		return r
	}
	return GitHubRenderer{}
}
