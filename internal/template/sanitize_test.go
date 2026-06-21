package template

import "testing"

func TestSanitizeSegmentFor(t *testing.T) {
	cases := []struct{ in, goos, want string }{
		{"v1.0.0", "linux", "v1.0.0"},
		{"v1.0.0", "windows", "v1.0.0"},
		{"release/1.0", "linux", "release-1.0"},
		{"release/1.0", "windows", "release-1.0"},
		{`a\b`, "linux", "a-b"},   // both path separators replaced everywhere
		{`a\b`, "windows", "a-b"}, //
		{"a:b*c?", "windows", "a-b-c-"},
		{"a:b", "linux", "a:b"}, // colon is valid on linux
		{"con", "windows", "con_"},
		{"COM1", "windows", "COM1_"},
		{"con", "linux", "con"}, // not reserved on linux
		{"name.", "windows", "name"}, // trailing dot trimmed
		{"name ", "windows", "name"}, // trailing space trimmed
		{"...", "windows", "tag"},     // empty after trim → fallback
		{".", "linux", "tag"},         // "." is the current dir → fallback
		{"..", "linux", "tag"},        // ".." is the parent dir → fallback
		{"", "linux", "tag"},          // empty → fallback
	}
	for _, c := range cases {
		if got := sanitizeSegmentFor(c.in, c.goos); got != c.want {
			t.Errorf("sanitizeSegmentFor(%q, %q) = %q, want %q", c.in, c.goos, got, c.want)
		}
	}
}
