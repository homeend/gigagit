package agentdocs

// Cold anchoring (spec 2026-10-18-gg-session-docs §4.3): a reader meets a
// note file without ever having seen the writer's copy of the file, so it
// places the note from its anchor lines alone. After this first placement
// the reader keeps its own content and re-anchors with reanchor as before.

// ColdPlace places anchor in file: at start when it is there, else at its
// one occurrence, else outdated at start..end clamped into the file (an
// empty file clamps to 1..1).
func ColdPlace(file []string, start, end int, anchor []string) (s, e int, outdated bool) {
	if at := relocate(file, anchor, start); at > 0 {
		return at, at + len(anchor) - 1, false
	}
	s, e = start, end
	if e > len(file) {
		e = len(file)
	}
	if s > e {
		s = e
	}
	if s < 1 {
		s, e = 1, 1
	}
	return s, e, true
}
