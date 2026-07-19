package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/domain"
)

// gitRun runs git in dir with a hermetic identity, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

type testEnv struct {
	dir string // repo worktree
	svc *domain.Service
	cs  *sdk.ClientSession
	sha string // seeded commit (full sha)
}

// newTestEnv builds a real one-commit repo (a.txt), isolates XDG state/config,
// and connects an in-memory MCP client to a fully registered server.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "seed: a.txt")
	sha := gitRun(t, dir, "rev-parse", "HEAD")

	svc := domain.Open(dir)
	srv := New(svc).sdkServer()
	ct, st := sdk.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return &testEnv{dir: dir, svc: svc, cs: cs, sha: sha}
}

func resultText(res *sdk.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// call invokes a tool and decodes its JSON reply, failing on any error.
func (e *testEnv) call(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := e.cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s: unexpected tool error: %s", name, resultText(res))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("%s: bad JSON reply %q: %v", name, resultText(res), err)
	}
	return out
}

// callErr invokes a tool expecting a tool error, returning its message.
func (e *testEnv) callErr(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	res, err := e.cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("%s: expected a tool error, got: %s", name, resultText(res))
	}
	return resultText(res)
}

func TestServerListsStageOneTools(t *testing.T) {
	e := newTestEnv(t)
	res, err := e.cs.ListTools(context.Background(), &sdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	want := []string{
		"gg_ui_state",
		"gg_bookmarks_list", "gg_bookmark_get", "gg_bookmark_read",
		"gg_shelf_buckets", "gg_shelf_list", "gg_shelf_commit_files", "gg_shelf_read",
		"gg_compare_trees", "gg_compare_file",
		"gg_export",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("tool %s not registered; got %v", name, got)
		}
	}
}

func TestEveryReplyCarriesRepoInfo(t *testing.T) {
	e := newTestEnv(t)
	out := e.call(t, "gg_ui_state", nil)
	repo, ok := out["repo"].(map[string]any)
	if !ok || repo["common_dir"] == "" || repo["worktree"] == "" {
		t.Fatalf("repo info missing: %v", out["repo"])
	}
}
