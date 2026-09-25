package e2e

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestScenarios discovers and runs every scenario file as a parallel subtest:
//
//	go test ./e2e -run 'TestScenarios/s01_switch_dirty_autostash' -v
func TestScenarios(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("scenarios", "*.toml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no scenario files found: %v", err)
	}
	for _, file := range files {
		t.Run(strings.TrimSuffix(filepath.Base(file), ".toml"), func(t *testing.T) {
			t.Parallel()
			sc, err := LoadScenario(file)
			if err != nil {
				t.Fatal(err)
			}
			t.Log(sc.Name) // shown with -v (and on failure): what this scenario verifies
			sb := buildSandbox(t, sc)
			var stdout, stderr bytes.Buffer
			for i, run := range sc.Runs {
				stdout.Reset()
				stderr.Reset()
				wd := sb.dir(run.Cwd)
				argv := ExpandArgs(run.Cmd, wd)
				code := (CLIRunner{}).Run(wd, argv, run.Stdin, &stdout, &stderr)
				if code != *run.Exit {
					// State past a failed run is unpredictable: stop here.
					t.Fatalf("run[%d] gg %s: exit %d, want %d\nstdout:\n%s\nstderr:\n%s",
						i, strings.Join(argv, " "), code, *run.Exit, stdout.String(), stderr.String())
				}
				if miss := run.MissingStdout(stdout.String()); len(miss) > 0 {
					t.Fatalf("run[%d] gg %s: stdout missing %v\nstdout:\n%s",
						i, strings.Join(argv, " "), miss, stdout.String())
				}
				if miss := run.MissingStderr(stderr.String()); len(miss) > 0 {
					t.Fatalf("run[%d] gg %s: stderr missing %v\nstderr:\n%s",
						i, strings.Join(argv, " "), miss, stderr.String())
				}
				if bad := run.PresentExcluded(stdout.String()); len(bad) > 0 {
					t.Fatalf("run[%d] gg %s: stdout unexpectedly contains %v\nstdout:\n%s",
						i, strings.Join(argv, " "), bad, stdout.String())
				}
				t.Logf("run[%d] gg %s → exit %d ✓", i, strings.Join(argv, " "), code)
			}
			assertExpect(t, sb, &sc.Expect)
		})
	}
}
