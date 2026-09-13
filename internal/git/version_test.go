package git

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestParseGitVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want [3]int
	}{
		{"git version 2.43.0", [3]int{2, 43, 0}},
		{"git version 2.39.3 (Apple Git-145)", [3]int{2, 39, 3}},
		{"git version 2.43.0.windows.1", [3]int{2, 43, 0}},
		{"git version 2.45\n", [3]int{2, 45, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseGitVersion(tc.in)
			if err != nil {
				t.Fatalf("ParseGitVersion(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseGitVersion(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseGitVersionRejectsGarbage(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "not a version", "git version x.y.z"} {
		if _, err := ParseGitVersion(in); err == nil {
			t.Errorf("ParseGitVersion(%q) = nil error, want an error", in)
		}
	}
}

func TestGitVersionArgv(t *testing.T) {
	t.Parallel()
	fake := gitexec.NewFakeRunner()
	fake.SetResponse("git version", gitexec.Result{Stdout: "git version 2.43.0\n"})
	r := &Repo{Runner: fake}

	got, err := r.GitVersion(context.Background())
	if err != nil {
		t.Fatalf("GitVersion: %v", err)
	}
	if want := ([3]int{2, 43, 0}); got != want {
		t.Errorf("GitVersion = %v, want %v", got, want)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("got %d git invocations, want exactly 1", len(fake.Calls))
	}
	if argv := fake.Calls[0].Argv; len(argv) == 0 || argv[0] != "version" {
		t.Errorf("argv = %v, want it to start with \"version\"", argv)
	}
}
