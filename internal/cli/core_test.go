package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/gittest"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

func newRepoDir(t *testing.T) string {
	t.Helper()
	return gittest.BasicRepo(t, "hi\n")
}

func TestCLIDeciderPolicyAnswers(t *testing.T) {
	d := cliDecider{policy: map[string]string{"non-fast-forward": "rebase"}}
	resp, err := d.Decide(context.Background(), engine.DecisionRequest{ID: "non-fast-forward"})
	if err != nil || resp.Option != "rebase" {
		t.Fatalf("resp=%v err=%v, want rebase", resp, err)
	}
}

func TestCLIDeciderNonInteractiveUnansweredErrors(t *testing.T) {
	d := cliDecider{policy: map[string]string{}, interactive: false}
	_, err := d.Decide(context.Background(), engine.DecisionRequest{ID: "non-fast-forward", Options: []string{"rebase", "abort"}})
	if err == nil {
		t.Fatal("non-interactive unanswered decision must error")
	}
}

func TestCLIDeciderInteractiveHonorsContextCancel(t *testing.T) {
	pr, pw := io.Pipe() // nothing ever writes: the prompt read blocks forever
	defer pw.Close()
	d := cliDecider{in: pr, out: &bytes.Buffer{}, interactive: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := d.Decide(ctx, engine.DecisionRequest{ID: "x", Prompt: "pick", Options: []string{"a", "b"}})
		done <- err
	}()
	time.Sleep(20 * time.Millisecond) // let Decide reach the blocking read
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Decide err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Decide stayed blocked on stdin after ctx cancel")
	}
}

func TestRunOperationCommitsAndStreamsProgress(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	svc := domain.Open(dir)

	var prog bytes.Buffer
	res, err := runOperation(context.Background(), svc, engine.Commit{Message: "second", All: true}, cliDecider{}, &prog)
	if err != nil {
		t.Fatalf("runOperation: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if !strings.Contains(prog.String(), "committing") {
		t.Fatalf("progress missing 'committing':\n%s", prog.String())
	}
}
