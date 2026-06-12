package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/observ"
)

// sinkLines parses every JSON span line the sink captured.
func sinkLines(t *testing.T, buf *bytes.Buffer) []observ.Span {
	t.Helper()
	var out []observ.Span
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if ln == "" {
			continue
		}
		var s observ.Span
		if err := json.Unmarshal([]byte(ln), &s); err != nil {
			t.Fatalf("bad span line %q: %v", ln, err)
		}
		out = append(out, s)
	}
	return out
}

func TestRunOperationEmitsOpSpan(t *testing.T) {
	var buf bytes.Buffer
	observ.SetSpanSink(&buf)
	t.Cleanup(func() { observ.SetSpanSink(nil) })

	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	repo := openRepo(dir)

	var prog bytes.Buffer
	if _, err := runOperation(context.Background(), repo, engine.Commit{Message: "x", All: true}, cliDecider{}, &prog); err != nil {
		t.Fatalf("runOperation: %v", err)
	}

	spans := sinkLines(t, &buf)
	var opSpan, gitSpan bool
	for _, s := range spans {
		if s.Name == "op Commit" && s.ExitCode == 0 && s.Duration > 0 {
			opSpan = true
		}
		if strings.HasPrefix(s.Name, "git commit") {
			gitSpan = true
		}
	}
	if !opSpan {
		t.Errorf("missing successful 'op Commit' span in %+v", spans)
	}
	if !gitSpan {
		t.Errorf("missing mirrored git subprocess span in %+v", spans)
	}
}

func TestRunOperationFailureSpanCarriesError(t *testing.T) {
	var buf bytes.Buffer
	observ.SetSpanSink(&buf)
	t.Cleanup(func() { observ.SetSpanSink(nil) })

	dir := newRepoDir(t) // clean tree: commit -a has nothing to commit -> error
	repo := openRepo(dir)
	var prog bytes.Buffer
	if _, err := runOperation(context.Background(), repo, engine.Commit{Message: "x", All: true}, cliDecider{}, &prog); err == nil {
		t.Fatal("expected the empty commit to fail")
	}

	for _, s := range sinkLines(t, &buf) {
		if s.Name == "op Commit" {
			if s.ExitCode != 1 || s.Err == "" {
				t.Fatalf("failure span = %+v, want ExitCode 1 with Err set", s)
			}
			return
		}
	}
	t.Fatal("no 'op Commit' span found")
}
