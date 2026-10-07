package forge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/forge/forgetest"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

// inputOf reads the --input file a mutation was handed (the handler runs
// before the GH method removes it).
func inputOf(t *testing.T, argv []string) map[string]any {
	t.Helper()
	i := slices.Index(argv, "--input")
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("argv %v has no --input file", argv)
	}
	b, err := os.ReadFile(argv[i+1])
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestGHWriterSendsEachMutationThroughAnInputFile(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	var docs []map[string]any
	var files []string
	answer := func(out string) func(context.Context, []string) (gitexec.Result, error) {
		return func(_ context.Context, argv []string) (gitexec.Result, error) {
			if argv[0] != "api" || argv[1] != "graphql" {
				t.Errorf("argv = %v", argv)
			}
			docs = append(docs, inputOf(t, argv))
			files = append(files, argv[slices.Index(argv, "--input")+1])
			return gitexec.Result{Stdout: out}, nil
		}
	}
	f.SetHandler("gh api graphql (StartReview)", answer(`{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"PRR_1"}}}}`))
	f.SetHandler("gh api graphql (AddThread)", answer(`{"data":{"addPullRequestReviewThread":{"thread":{"id":"PRRT_1","comments":{"nodes":[{"id":"PRRC_1","url":"https://x/1"}]}}}}}`))
	f.SetHandler("gh api graphql (Reply)", answer(`{"data":{"addPullRequestReviewThreadReply":{"comment":{"id":"PRRC_2","url":"https://x/2"}}}}`))
	f.SetHandler("gh api graphql (SubmitReview)", answer(`{"data":{"submitPullRequestReview":{"pullRequestReview":{"id":"PRR_1","state":"COMMENTED"}}}}`))
	g := NewGHWithRunner(f)
	ctx := context.Background()
	id, err := g.StartReview(ctx, "PR_kw7", "h1h1")
	if err != nil || id != "PRR_1" {
		t.Fatalf("StartReview = %q, %v", id, err)
	}
	ref, err := g.AddThread(ctx, id, Thread{Path: "a.go", Line: 12, StartLine: 10, Side: model.NoteSideNew, Body: "why?"})
	if err != nil || ref != (ThreadRef{ID: "PRRT_1", CommentID: "PRRC_1", URL: "https://x/1"}) {
		t.Fatalf("AddThread = %+v, %v", ref, err)
	}
	if _, err := g.AddThread(ctx, id, Thread{Path: "b.go", Body: "whole file"}); err != nil {
		t.Fatal(err)
	}
	c, err := g.Reply(ctx, id, "PRRT_1", "and also")
	if err != nil || c.ID != "PRRC_2" {
		t.Fatalf("Reply = %+v, %v", c, err)
	}
	if err := g.SubmitReview(ctx, id, EventApprove, "lgtm"); err != nil {
		t.Fatal(err)
	}
	vars := func(i int) map[string]any { return docs[i]["variables"].(map[string]any) }
	if q := docs[0]["query"].(string); !strings.HasPrefix(q, "mutation StartReview(") || vars(0)["pr"] != "PR_kw7" || vars(0)["commit"] != "h1h1" {
		t.Errorf("StartReview doc = %v", docs[0])
	}
	if v := vars(1); v["review"] != "PRR_1" || v["path"] != "a.go" || v["line"] != 12.0 || v["startLine"] != 10.0 ||
		v["side"] != "RIGHT" || v["startSide"] != "RIGHT" || v["subject"] != "LINE" {
		t.Errorf("line thread vars = %v", v)
	}
	if v := vars(2); v["subject"] != "FILE" || v["line"] != nil || v["side"] != nil {
		t.Errorf("file thread vars = %v (a file-level thread carries no line or side)", v)
	}
	if v := vars(3); v["thread"] != "PRRT_1" || v["review"] != "PRR_1" || v["body"] != "and also" {
		t.Errorf("reply vars = %v", v)
	}
	if v := vars(4); v["event"] != "APPROVE" || v["body"] != "lgtm" {
		t.Errorf("submit vars = %v", v)
	}
	for _, p := range files {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("input file %s left behind", p)
		}
	}
}

func TestGHWriterOneLineThreadSendsNoStartLine(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	var doc map[string]any
	f.SetHandler("gh api graphql (AddThread)", func(_ context.Context, argv []string) (gitexec.Result, error) {
		doc = inputOf(t, argv)
		return gitexec.Result{Stdout: `{"data":{"addPullRequestReviewThread":{"thread":{"id":"T","comments":{"nodes":[]}}}}}`}, nil
	})
	if _, err := NewGHWithRunner(f).AddThread(context.Background(), "R",
		Thread{Path: "a.go", Line: 3, StartLine: 3, Side: model.NoteSideOld, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	v := doc["variables"].(map[string]any)
	if v["side"] != "LEFT" || v["startLine"] != nil || v["startSide"] != nil {
		t.Errorf("vars = %v", v)
	}
}

func TestGHWriterSurfacesGraphQLErrors(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetHandler("gh api graphql (SubmitReview)", func(context.Context, []string) (gitexec.Result, error) {
		return gitexec.Result{Stdout: `{"errors":[{"message":"Body can't be blank"}]}`, Stderr: "gh: Body can't be blank"},
			errors.New("exit status 1")
	})
	err := NewGHWithRunner(f).SubmitReview(context.Background(), "R", EventComment, "")
	if err == nil || !strings.Contains(err.Error(), "Body can't be blank") || !IsBlankBodyError(err) {
		t.Fatalf("err = %v", err)
	}
	f.SetHandler("gh api graphql (Resolve)", func(context.Context, []string) (gitexec.Result, error) {
		return gitexec.Result{Stdout: `{"data":null,"errors":[{"message":"Resource not accessible by integration"}]}`}, nil
	})
	if err := NewGHWithRunner(f).Resolve(context.Background(), "T"); err == nil || IsBlankBodyError(err) {
		t.Fatalf("a GraphQL error on exit 0 must still fail: %v", err)
	}
}

func TestSendMarker(t *testing.T) {
	t.Parallel()
	body := "why?\n\n" + SendMarker("a1b2c3d4")
	if k, ok := SendMarkerKey(body); !ok || k != "a1b2c3d4" {
		t.Fatalf("SendMarkerKey = %q, %v", k, ok)
	}
	if got := StripSendMarker(body); got != "why?" {
		t.Fatalf("StripSendMarker = %q", got)
	}
	if k, ok := SendMarkerKey("plain <!-- note -->"); ok {
		t.Fatalf("a foreign HTML comment is not a marker: %q", k)
	}
	if got := StripSendMarker("no marker"); got != "no marker" {
		t.Fatalf("StripSendMarker changed an unmarked body: %q", got)
	}
}

func TestReviewWithOnlyAMarkerIsAnEmptyEnvelope(t *testing.T) {
	t.Parallel()
	doc := `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{},"nodes":[]},"comments":{"pageInfo":{},"nodes":[]},
"reviews":{"pageInfo":{},"nodes":[{"id":"R1","author":{"login":"me"},"body":"<!-- gg:n1 -->","state":"COMMENTED","submittedAt":"2026-10-07T10:00:00Z"}]}}}}}`
	cs, _, err := parseThreads([]byte(doc))
	if err != nil || len(cs) != 0 {
		t.Fatalf("parseThreads = %+v, %v (a marker-only COMMENTED review is the bare envelope)", cs, err)
	}
}

// Serial: t.Setenv. The real subprocess path against the fake binary: every
// mutation is recorded, a fail-<Op> file fails it.
func TestGHWriterAgainstFakeBinary(t *testing.T) {
	bin := forgetest.BuildFakeGH(t)
	dir := t.TempDir()
	t.Setenv(forgetest.EnvFixtures, dir)
	t.Setenv(EnvBin, bin)
	g := NewGH(t.TempDir(), nil)
	ctx := context.Background()
	id, err := g.StartReview(ctx, "PR_kw7", "h1h1")
	if err != nil || id == "" {
		t.Fatalf("StartReview = %q, %v", id, err)
	}
	ref, err := g.AddThread(ctx, id, Thread{Path: "a.go", Line: 4, Side: model.NoteSideNew, Body: "x"})
	if err != nil || ref.ID == "" || ref.CommentID == "" {
		t.Fatalf("AddThread = %+v, %v", ref, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail-SubmitReview"), []byte("GraphQL: Body can't be blank"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.SubmitReview(ctx, id, EventComment, ""); !IsBlankBodyError(err) {
		t.Fatalf("SubmitReview err = %v", err)
	}
	ws := forgetest.Writes(t, dir)
	var ops []string
	for _, w := range ws {
		ops = append(ops, w.Op)
	}
	if strings.Join(ops, ",") != "StartReview,AddThread,SubmitReview" {
		t.Fatalf("writes = %v", ops)
	}
	if ws[1].Vars["path"] != "a.go" {
		t.Errorf("AddThread vars = %v", ws[1].Vars)
	}
}
