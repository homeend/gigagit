package cli

import (
	"strings"
	"testing"
)

// Task 10: `gg link --pr <n>` prints PR n's link — its base against gg's
// private head ref — alone or to a file/line in it; one target only.
// Serial: sendPRRepo sets the fake gh with t.Setenv.
func TestLinkPR(t *testing.T) {
	dir, _, _ := sendPRRepo(t)
	run := func(args ...string) (int, string, string) {
		var out, errb strings.Builder
		code := Run(dir, append([]string{"link"}, args...), strings.NewReader(""), &out, &errb, "")
		return code, out.String(), errb.String()
	}
	if code, out, errs := run("--pr", "7"); code != 0 || !strings.HasSuffix(out, "@main...refs/gg/pr/7\n") {
		t.Fatalf("--pr 7 = %d %q %q", code, out, errs)
	}
	if code, out, errs := run("--pr", "7", "big.go:5"); code != 0 || !strings.HasSuffix(out, "/big.go@main...refs/gg/pr/7:5\n") {
		t.Fatalf("--pr 7 big.go:5 = %d %q %q", code, out, errs)
	}
	if code, _, _ := run("--pr", "7", "--rev", "HEAD"); code != 2 {
		t.Fatalf("--pr with --rev = %d, want 2", code)
	}
	if code, _, _ := run("--pr", "x"); code != 2 {
		t.Fatalf("--pr x = %d, want 2", code)
	}
	if code, _, errs := run("--pr", "8"); code != 1 || !strings.Contains(errs, "gg pr fetch 8") {
		t.Fatalf("--pr 8 = %d %q", code, errs)
	}
}
