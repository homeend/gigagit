package buildinfo

import "testing"

func TestStringIncludesVersion(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })
	Version = "9.9.9"
	if got := String(); got == "" {
		t.Fatal("String() returned empty")
	}
	if want := "9.9.9"; !contains(String(), want) {
		t.Fatalf("String() = %q, want it to contain %q", String(), want)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
