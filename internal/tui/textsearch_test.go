package tui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// srchLines is one searchable line per text, numbered in order. (Named for the
// search, not "lines", so it cannot shadow a local `lines` in a sibling test.)
func srchLines(texts ...string) []searchLine {
	out := make([]searchLine, len(texts))
	for i, t := range texts {
		out[i] = searchLine{row: i, side: 1, text: t}
	}
	return out
}

func TestFindHitsCaseInsensitiveAndOrdered(t *testing.T) {
	t.Parallel()
	got := findHits(srchLines("Alpha beta", "no match", "alphaALPHA"), "alpha")
	want := []searchHit{
		{row: 0, side: 1, start: 0, end: 5},
		{row: 2, side: 1, start: 0, end: 5},
		{row: 2, side: 1, start: 5, end: 10},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findHits = %v, want %v", got, want)
	}
	if findHits(srchLines("anything"), "") != nil {
		t.Fatal("an empty query matches nothing")
	}
}

// Offsets must be RUNE offsets into the display string: a multi-byte rune
// before the hit (byte offsets would drift) and a wide glyph inside it.
func TestFindHitsCountsRunesNotBytes(t *testing.T) {
	t.Parallel()
	got := findHits(srchLines("日本語 alpha"), "alpha")
	if len(got) != 1 || got[0].start != 4 || got[0].end != 9 {
		t.Fatalf("hit = %v, want one hit at runes [4,9)", got)
	}
	got = findHits(srchLines("x漢字y"), "漢字")
	if len(got) != 1 || got[0].start != 1 || got[0].end != 3 {
		t.Fatalf("wide-glyph hit = %v, want [1,3)", got)
	}
}

// Overlapping matches are not reported: the walk advances past each hit.
func TestFindHitsDoesNotOverlap(t *testing.T) {
	t.Parallel()
	got := findHits(srchLines("aaaa"), "aa")
	if len(got) != 2 || got[0].start != 0 || got[1].start != 2 {
		t.Fatalf("hits = %v, want [0,2) and [2,4)", got)
	}
}

func TestNearestHitSnapsForwardAndBackwardWithWrap(t *testing.T) {
	t.Parallel()
	hits := []searchHit{
		{row: 1, side: 0, start: 2, end: 5},
		{row: 4, side: 1, start: 0, end: 3},
	}
	if got := nearestHit(hits, searchPos{row: 2}, false); got != 1 {
		t.Fatalf("forward from row 2 = %d, want 1", got)
	}
	if got := nearestHit(hits, searchPos{row: 9}, false); got != 0 {
		t.Fatalf("forward past the end must wrap to 0, got %d", got)
	}
	if got := nearestHit(hits, searchPos{row: 2}, true); got != 0 {
		t.Fatalf("backward from row 2 = %d, want 0", got)
	}
	if got := nearestHit(hits, searchPos{row: 0}, true); got != 1 {
		t.Fatalf("backward before the start must wrap to the last, got %d", got)
	}
	if got := nearestHit(nil, searchPos{}, false); got != -1 {
		t.Fatalf("no hits = %d, want -1", got)
	}
}

// ] is the first hit STRICTLY after the position, [ the last strictly before —
// so sitting exactly on a hit steps off it. Both wrap.
func TestStepHitIsStrictAndWraps(t *testing.T) {
	t.Parallel()
	hits := []searchHit{
		{row: 1, side: 0, start: 2, end: 5},
		{row: 1, side: 1, start: 0, end: 3},
		{row: 4, side: 1, start: 7, end: 10},
	}
	on := searchPos{row: 1, side: 0, col: 2}
	if got := stepHit(hits, on, 1); got != 1 {
		t.Fatalf("next from hit 0 = %d, want 1 (left side steps to the right side)", got)
	}
	if got := stepHit(hits, on, -1); got != 2 {
		t.Fatalf("prev from hit 0 must wrap to the last, got %d", got)
	}
	last := searchPos{row: 4, side: 1, col: 7}
	if got := stepHit(hits, last, 1); got != 0 {
		t.Fatalf("next from the last must wrap to 0, got %d", got)
	}
	if got := stepHit(nil, on, 1); got != -1 {
		t.Fatalf("no hits = %d, want -1", got)
	}
}

func TestHitsOnReturnsSpansAndMarksCurrent(t *testing.T) {
	t.Parallel()
	s := textSearch{
		query: "a",
		hits: []searchHit{
			{row: 1, side: 0, start: 0, end: 1},
			{row: 1, side: 1, start: 3, end: 4},
			{row: 2, side: 1, start: 5, end: 6},
		},
		cur: 1,
	}
	got := s.hitsOn(1, 1)
	want := []hitSpan{{start: 3, end: 4, cur: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hitsOn(1,1) = %v, want %v", got, want)
	}
	if got := s.hitsOn(1, 0); len(got) != 1 || got[0].cur {
		t.Fatalf("hitsOn(1,0) = %v, want one non-current span", got)
	}
	if got := s.hitsOn(3, 1); got != nil {
		t.Fatalf("a row with no hits must return nil, got %v", got)
	}
}

func TestBadge(t *testing.T) {
	t.Parallel()
	var s textSearch
	if s.badge() != "" {
		t.Fatalf("an inactive search has no badge, got %q", s.badge())
	}
	s = textSearch{query: "foo", typing: true, cur: -1}
	if got := s.badge(); got != "/foo█  0/0" {
		t.Fatalf("typing badge = %q", got)
	}
	s = textSearch{query: "foo", backward: true, hits: make([]searchHit, 12), cur: 2}
	if got := s.badge(); got != "@foo  3/12" {
		t.Fatalf("backward badge = %q, want %q", got, "@foo  3/12")
	}
	s = textSearch{typing: true, cur: -1}
	if got := s.badge(); got != "/█" {
		t.Fatalf("empty typing badge = %q", got)
	}
}

func TestPanForMovesMinimally(t *testing.T) {
	t.Parallel()
	// Already inside the window: no pan.
	if got := panFor(10, 20, 12, 16); got != 10 {
		t.Fatalf("visible hit panned to %d, want 10", got)
	}
	// Left of the window: the hit's start becomes the left edge.
	if got := panFor(10, 20, 4, 8); got != 4 {
		t.Fatalf("pan left = %d, want 4", got)
	}
	// Right of the window: the hit's end becomes the right edge.
	if got := panFor(10, 20, 34, 38); got != 18 {
		t.Fatalf("pan right = %d, want 18", got)
	}
	// A hit wider than the window shows its head.
	if got := panFor(0, 10, 40, 80); got != 40 {
		t.Fatalf("oversized hit = %d, want 40", got)
	}
	if got := panFor(5, 20, 0, 2); got != 0 {
		t.Fatalf("pan to the left edge = %d, want 0", got)
	}
}

// Display COLUMNS, not rune indexes: a wide glyph before the hit occupies two.
func TestHitColsUsesDisplayColumns(t *testing.T) {
	t.Parallel()
	start, end := hitCols("漢字abc", searchHit{start: 2, end: 5})
	if start != 4 || end != 7 {
		t.Fatalf("hitCols = (%d,%d), want (4,7)", start, end)
	}
}

func TestSearchCommandKey(t *testing.T) {
	t.Parallel()
	var s textSearch
	if got := searchCommandKey(&s, keyMsg("/")); got != searchOpenFwd {
		t.Fatalf("/ = %v", got)
	}
	if got := searchCommandKey(&s, keyMsg("@")); got != searchOpenBack {
		t.Fatalf("@ = %v", got)
	}
	// No query: the stepping keys are inert and esc is not ours.
	if got := searchCommandKey(&s, keyMsg("]")); got != searchIgnored {
		t.Fatalf("] with no query = %v, want searchIgnored", got)
	}
	if got := searchCommandKey(&s, keyMsg("esc")); got != searchNotOurs {
		t.Fatalf("esc with no query = %v, want searchNotOurs", got)
	}
	s.query = "x"
	if got := searchCommandKey(&s, keyMsg("]")); got != searchNext {
		t.Fatalf("] = %v", got)
	}
	if got := searchCommandKey(&s, keyMsg("[")); got != searchPrev {
		t.Fatalf("[ = %v", got)
	}
	if got := searchCommandKey(&s, keyMsg("esc")); got != searchCleared {
		t.Fatalf("esc with a query = %v, want searchCleared", got)
	}
	if got := searchCommandKey(&s, keyMsg("j")); got != searchNotOurs {
		t.Fatalf("an unrelated key = %v, want searchNotOurs", got)
	}
}

func TestSearchTypingKeyBuildsAndCommits(t *testing.T) {
	t.Parallel()
	m := Model{}
	s := &textSearch{typing: true, cur: -1}

	m, _, ev := m.searchTypingKey(s, keyMsg("a"))
	if ev != searchChanged || s.query != "a" {
		t.Fatalf("rune: ev=%v query=%q", ev, s.query)
	}
	m, _, ev = m.searchTypingKey(s, keyType(tea.KeySpace))
	if ev != searchChanged || s.query != "a " {
		t.Fatalf("space: ev=%v query=%q", ev, s.query)
	}
	m, _, ev = m.searchTypingKey(s, keyType(tea.KeyBackspace))
	if ev != searchChanged || s.query != "a" {
		t.Fatalf("backspace: ev=%v query=%q", ev, s.query)
	}
	// Every other key is swallowed while typing: j is query text, not motion.
	_, _, ev = m.searchTypingKey(s, keyType(tea.KeyTab))
	if ev != searchIgnored {
		t.Fatalf("tab while typing = %v, want searchIgnored", ev)
	}
	_, _, ev = m.searchTypingKey(s, keyType(tea.KeyEnter))
	if ev != searchCommitted || s.typing {
		t.Fatalf("enter: ev=%v typing=%v", ev, s.typing)
	}
}

func TestSearchTypingKeyEscCancelsAndEmptyEnterCancels(t *testing.T) {
	t.Parallel()
	m := Model{}
	s := &textSearch{typing: true, query: "ab", hits: make([]searchHit, 2), cur: 1}
	_, _, ev := m.searchTypingKey(s, keyType(tea.KeyEsc))
	if ev != searchCancelled || s.query != "" || s.typing || s.hits != nil || s.cur != -1 {
		t.Fatalf("esc: ev=%v state=%+v", ev, *s)
	}

	s = &textSearch{typing: true, cur: -1}
	_, _, ev = m.searchTypingKey(s, keyType(tea.KeyEnter))
	if ev != searchCancelled || s.typing {
		t.Fatalf("enter on an empty query = %v (typing=%v), want searchCancelled", ev, s.typing)
	}
}

// alt+↓ opens the shared in-view ring; the previewed phrase becomes the query
// and the host must re-find (searchChanged).
func TestSearchTypingKeyRecallPreviewsTheRing(t *testing.T) {
	t.Parallel()
	m := Model{searchHist: map[string][]string{scopeInView: {"older"}}}
	s := &textSearch{typing: true, query: "ol", cur: -1}
	m, _, ev := m.searchTypingKey(s, keyMsg("alt+down"))
	if ev != searchChanged || s.query != "older" {
		t.Fatalf("recall: ev=%v query=%q", ev, s.query)
	}
	if !m.recallOpen {
		t.Fatal("the dropdown must be open")
	}
	// esc with the dropdown OPEN closes it and restores the draft — it must NOT
	// cancel the search.
	m, _, ev = m.searchTypingKey(s, keyType(tea.KeyEsc))
	if ev != searchChanged || s.query != "ol" || !s.typing {
		t.Fatalf("esc under recall: ev=%v query=%q typing=%v", ev, s.query, s.typing)
	}
	if m.recallOpen {
		t.Fatal("the dropdown must have closed")
	}
}

func TestRefindFromSnapsToTheNearestHit(t *testing.T) {
	t.Parallel()
	s := &textSearch{query: "a"}
	s.refindFrom(srchLines("xa", "xx", "aa"), searchPos{row: 1})
	if len(s.hits) != 3 {
		t.Fatalf("hits = %v, want 3", s.hits)
	}
	if s.cur != 1 {
		t.Fatalf("cur = %d, want 1 (the first hit at or after row 1)", s.cur)
	}
	s.query = "zzz"
	s.refindFrom(srchLines("xa"), searchPos{})
	if len(s.hits) != 0 || s.cur != -1 {
		t.Fatalf("a query with no match must leave no hits and cur=-1: %v %d", s.hits, s.cur)
	}
}
