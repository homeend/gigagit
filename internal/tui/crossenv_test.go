package tui

import (
	"errors"
	"testing"
)

func TestTranslatePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		goos, in, want string
		ok             bool
	}{
		// windows: WSL-recorded /mnt/<x>/… → <X>:\…
		{"windows", "/mnt/t/others/gigagit", `T:\others\gigagit`, true},
		{"windows", "/mnt/c/Users/x", `C:\Users\x`, true},
		{"windows", "/mnt/t", `T:\`, true},           // bare mount root → drive root, never a drive-relative "T:"
		{"windows", "/mnt/", "", false},              // no drive letter
		{"windows", "/mnt/tt/x", "", false},          // multi-letter mount is not a drive
		{"windows", "/home/user/repo", "", false},    // plain Linux path: no counterpart
		{"windows", `T:\already\windows`, "", false}, // already native
		// linux (WSL): Windows-recorded <X>:… → /mnt/<x>/…
		{"linux", `T:\others\gigagit`, "/mnt/t/others/gigagit", true},
		{"linux", "T:/others/gigagit", "/mnt/t/others/gigagit", true}, // forward-slash Windows form
		{"linux", `t:\lower`, "/mnt/t/lower", true},
		{"linux", "T:", "/mnt/t", true},
		{"linux", "/mnt/t/others", "", false}, // already native
		{"linux", `1:\x`, "", false},          // not a drive letter
		// any other GOOS: never translatable
		{"darwin", `T:\x`, "", false},
		{"darwin", "/mnt/t/x", "", false},
	}
	for _, c := range cases {
		got, ok := translatePath(c.goos, c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("translatePath(%q, %q) = (%q, %v), want (%q, %v)", c.goos, c.in, got, ok, c.want, c.ok)
		}
	}
}

// statSet returns a stat func that succeeds only for the listed paths.
func statSet(ok ...string) func(string) error {
	set := map[string]bool{}
	for _, p := range ok {
		set[p] = true
	}
	return func(p string) error {
		if set[p] {
			return nil
		}
		return errors.New("stat: missing")
	}
}

func TestCheckSwitchTarget(t *testing.T) {
	t.Parallel()
	if v, p := checkSwitchTarget(statSet("/x"), "linux", "/x"); v != switchOK || p != "/x" {
		t.Fatalf("reachable: got (%v, %q)", v, p)
	}
	// Recorded in WSL notation, reachable under the Windows translation.
	if v, p := checkSwitchTarget(statSet(`T:\x`), "windows", "/mnt/t/x"); v != switchRepairable || p != `T:\x` {
		t.Fatalf("repairable: got (%v, %q)", v, p)
	}
	// Neither the path nor its translation exists.
	if v, p := checkSwitchTarget(statSet(), "windows", "/mnt/t/x"); v != switchUnreachable || p != "" {
		t.Fatalf("unreachable+translatable: got (%v, %q)", v, p)
	}
	// Not translatable at all (deleted native dir).
	if v, p := checkSwitchTarget(statSet(), "linux", "/gone"); v != switchUnreachable || p != "" {
		t.Fatalf("unreachable: got (%v, %q)", v, p)
	}
	// On non-WSL Linux a C:\… string translates but the mount never stats.
	if v, _ := checkSwitchTarget(statSet(), "linux", `C:\repo`); v != switchUnreachable {
		t.Fatalf("non-WSL linux: got %v", v)
	}
}
