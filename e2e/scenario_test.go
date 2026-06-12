package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeScenario(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const minimalScenario = `
name = "minimal"
[input]
steps = [
  { write = "a.txt", content = "v1\n" },
  { commit = "initial" },
]
[[run]]
cmd  = ["status"]
exit = 0
[expect]
branch = "main"
clean  = true
[expect.files]
"a.txt" = "v1\n"
"b.txt" = { absent = true }
"c.txt" = { sha256 = "ab12" }
"d.txt" = { unchanged = true }
`

func TestLoadMinimalScenario(t *testing.T) {
	sc, err := LoadScenario(writeScenario(t, minimalScenario))
	if err != nil {
		t.Fatalf("LoadScenario: %v", err)
	}
	if sc.Name != "minimal" || len(sc.Input.Steps) != 2 || len(sc.Runs) != 1 {
		t.Fatalf("unexpected scenario: %+v", sc)
	}
	if sc.Runs[0].Exit == nil || *sc.Runs[0].Exit != 0 {
		t.Fatalf("run exit not parsed: %+v", sc.Runs[0])
	}
	fe := sc.Expect.FilesN
	if fe["a.txt"].Content != "v1\n" || !fe["b.txt"].Absent || fe["c.txt"].SHA256 != "ab12" || !fe["d.txt"].Unchanged {
		t.Fatalf("file expectations not normalized: %+v", fe)
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	_, err := LoadScenario(writeScenario(t, `
name = "x"
bogus = true
[input]
steps = [ { commit = "c" } ]
[[run]]
cmd = ["status"]
exit = 0
`))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("want unknown-key error mentioning bogus, got %v", err)
	}
}

func TestStepMustHaveExactlyOneAction(t *testing.T) {
	for _, step := range []string{
		`{ write = "a", commit = "c" }`, // two actions
		`{ content = "x" }`,             // content without write
		`{ cwd = "local" }`,             // no action at all
		`{}`,                            // empty table → zero Step → must fail kind()
		`{ worktree = "wt-x" }`,         // worktree without branch must be rejected
	} {
		_, err := LoadScenario(writeScenario(t, `
name = "x"
[input]
steps = [ { commit = "seed" }, `+step+` ]
[[run]]
cmd = ["status"]
exit = 0
`))
		if err == nil {
			t.Errorf("step %s: want validation error, got nil", step)
		}
	}
}

func TestRunRequiresCmdAndExit(t *testing.T) {
	for _, run := range []string{
		"[[run]]\ncmd = [\"status\"]", // no exit
		"[[run]]\nexit = 0",           // no cmd
	} {
		_, err := LoadScenario(writeScenario(t, "name = \"x\"\n[input]\nsteps = [ { commit = \"c\" } ]\n"+run+"\n"))
		if err == nil {
			t.Errorf("run %q: want validation error, got nil", run)
		}
	}
}

func TestAheadRequiresOrigin(t *testing.T) {
	_, err := LoadScenario(writeScenario(t, `
name = "x"
[input]
steps = [ { commit = "c" } ]
[[run]]
cmd = ["status"]
exit = 0
[expect]
ahead = 1
`))
	if err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("want ahead-requires-origin error, got %v", err)
	}
}

func TestInputRequiresACommit(t *testing.T) {
	_, err := LoadScenario(writeScenario(t, `
name = "x"
[input]
steps = [ { write = "a.txt", content = "x" } ]
[[run]]
cmd = ["status"]
exit = 0
`))
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("want needs-a-commit error, got %v", err)
	}
}

func TestOriginRequiresCommitInOriginSteps(t *testing.T) {
	// origin present, but all commits only in input.steps → invalid
	_, err := LoadScenario(writeScenario(t, `
name = "x"
[input]
steps = [ { write = "a.txt", content = "x" }, { commit = "local" } ]
[input.origin]
steps = [ { write = "b.txt", content = "y" } ]
[[run]]
cmd = ["status"]
exit = 0
`))
	if err == nil || !strings.Contains(err.Error(), "origin.steps") {
		t.Fatalf("want origin.steps-needs-commit error, got %v", err)
	}
}

func TestLogSubjectsNormalize(t *testing.T) {
	sc, err := LoadScenario(writeScenario(t, `
name = "x"
[input]
steps = [ { commit = "c" } ]
[[run]]
cmd = ["status"]
exit = 0
[[expect.log]]
subjects = ["literal", { matches = "^Merge" }]
`))
	if err != nil {
		t.Fatalf("LoadScenario: %v", err)
	}
	ms := sc.Expect.Log[0].SubjectsN
	if len(ms) != 2 || ms[0].Literal != "literal" || ms[1].Pattern == nil {
		t.Fatalf("subject matchers not normalized: %+v", ms)
	}
}
