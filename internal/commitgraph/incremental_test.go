package commitgraph

import "testing"

// A chunked incremental layout (Layer.Append per page) must produce the same
// per-row glyphs and node lanes as one Lay over all commits — the property that
// lets the TUI page in older commits without re-laying the whole history.
func TestLayerAppendMatchesLay(t *testing.T) {
	// A history with a merge, a fork, and a root, laid across arbitrary chunks.
	commits := []Commit{
		{Hash: "h", Parents: []string{"g", "f"}}, // merge
		{Hash: "g", Parents: []string{"e"}},
		{Hash: "f", Parents: []string{"d"}},
		{Hash: "e", Parents: []string{"c"}},
		{Hash: "d", Parents: []string{"c"}},
		{Hash: "c", Parents: []string{"b"}},
		{Hash: "b", Parents: []string{"a"}},
		{Hash: "a", Parents: nil}, // root
	}
	want, wantW := Lay(commits)

	for _, chunk := range []int{1, 2, 3, 5} {
		var l Layer
		var got []Row
		for i := 0; i < len(commits); i += chunk {
			end := i + chunk
			if end > len(commits) {
				end = len(commits)
			}
			got = append(got, l.Append(commits[i:end])...)
		}
		// Pad the incrementally-laid rows to the final plane width, as the caller does.
		w := l.Width()
		if w != wantW {
			t.Fatalf("chunk=%d: Width()=%d, want %d", chunk, w, wantW)
		}
		for i := range got {
			if got[i].Lane != want[i].Lane {
				t.Fatalf("chunk=%d row %d: Lane=%d, want %d", chunk, i, got[i].Lane, want[i].Lane)
			}
			padded := got[i].Cells
			for len([]rune(padded)) < w {
				padded += " "
			}
			if padded != want[i].Cells {
				t.Fatalf("chunk=%d row %d:\n got %q\nwant %q", chunk, i, padded, want[i].Cells)
			}
		}
	}
}

// Width only grows across Append calls (never shrinks), so already-emitted rows
// never need to be re-fit narrower.
func TestLayerWidthMonotonic(t *testing.T) {
	commits := []Commit{
		{Hash: "c", Parents: []string{"a", "b"}}, // opens a second lane
		{Hash: "b", Parents: []string{"a"}},
		{Hash: "a", Parents: nil},
	}
	var l Layer
	prev := 0
	for _, c := range commits {
		l.Append([]Commit{c})
		if l.Width() < prev {
			t.Fatalf("Width shrank: %d < %d", l.Width(), prev)
		}
		prev = l.Width()
	}
}
