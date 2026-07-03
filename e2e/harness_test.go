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
				code := (CLIRunner{}).Run(sb.dir(run.Cwd), run.Cmd, run.Stdin, &stdout, &stderr)
				if code != *run.Exit {
					// State past a failed run is unpredictable: stop here.
					t.Fatalf("run[%d] gg %s: exit %d, want %d\nstdout:\n%s\nstderr:\n%s",
						i, strings.Join(run.Cmd, " "), code, *run.Exit, stdout.String(), stderr.String())
				}
				if miss := run.MissingStdout(stdout.String()); len(miss) > 0 {
					t.Fatalf("run[%d] gg %s: stdout missing %v\nstdout:\n%s",
						i, strings.Join(run.Cmd, " "), miss, stdout.String())
				}
				if bad := run.PresentExcluded(stdout.String()); len(bad) > 0 {
					t.Fatalf("run[%d] gg %s: stdout unexpectedly contains %v\nstdout:\n%s",
						i, strings.Join(run.Cmd, " "), bad, stdout.String())
				}
				t.Logf("run[%d] gg %s → exit %d ✓", i, strings.Join(run.Cmd, " "), code)
			}
			assertExpect(t, sb, &sc.Expect)
		})
	}
}
