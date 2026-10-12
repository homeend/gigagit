package notemeta

import (
	"reflect"
	"strings"
	"testing"
)

func TestCheckMetaKeepsValidDropsTheRest(t *testing.T) {
	t.Parallel()
	in := []KV{
		{"kind", "issue"},
		{"kind", "note"},                   // duplicate: first wins
		{"", "x"},                          // empty key
		{"two\nlines", "x"},                // multi-line key
		{"intent", "a\nb"},                 // multi-line value
		{strings.Repeat("k", 65), "x"},     // key too long
		{"long", strings.Repeat("v", 257)}, // value too long
		{"severity", "high"},
	}
	got, probs := CheckMeta(in)
	want := []KV{{"kind", "issue"}, {"severity", "high"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kept = %v, want %v", got, want)
	}
	if len(probs) != 6 {
		t.Fatalf("problems = %v, want 6", probs)
	}
}

func TestCheckMetaCapsAt32Keys(t *testing.T) {
	t.Parallel()
	var in []KV
	for i := 0; i < 40; i++ {
		in = append(in, KV{Key: "k" + string(rune('A'+i)), Value: "v"})
	}
	got, probs := CheckMeta(in)
	if len(got) != MaxMetaKeys || len(probs) != 8 {
		t.Fatalf("kept %d, problems %d; want 32, 8", len(got), len(probs))
	}
}

func TestSplitTags(t *testing.T) {
	t.Parallel()
	plain, meta := SplitTags([]string{"review", "severity: risk", "perf", "ruling: confirmed (as nit)", "a b: c", "resolved"})
	if !reflect.DeepEqual(plain, []string{"review", "perf", "a b: c", "resolved"}) {
		t.Fatalf("plain = %v", plain)
	}
	if !reflect.DeepEqual(meta, []KV{{"severity", "risk"}, {"ruling", "confirmed (as nit)"}}) {
		t.Fatalf("meta = %v", meta)
	}
}

func TestConfidenceWord(t *testing.T) {
	t.Parallel()
	for f, w := range map[float64]string{0.3: "low", 0.6: "medium", 0.9: "high", 0.5: "0.5"} {
		if got := ConfidenceWord(f); got != w {
			t.Errorf("ConfidenceWord(%v) = %q, want %q", f, got, w)
		}
	}
}

func TestGlyphOK(t *testing.T) {
	t.Parallel()
	for _, g := range []string{"!", "~", "A", "◆", "·", "→", "»", "•"} {
		if !GlyphOK(g) {
			t.Errorf("GlyphOK(%q) = false", g)
		}
	}
	for _, g := range []string{"", " ", "✓", "✗", "ab", "🔥", "\t"} {
		if GlyphOK(g) {
			t.Errorf("GlyphOK(%q) = true", g)
		}
	}
}
