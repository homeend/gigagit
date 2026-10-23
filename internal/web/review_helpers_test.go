package web

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// Review fixtures shared by the preview-review and launch tests (the review
// lane they were written for is gone: reviews run as AI tasks).

// reviewRepo builds a two-branch repo whose .gg.toml declares one review
// tool. body is the command; it runs through the same capture runner a real
// agent would.
func reviewRepo(t *testing.T, tools string) string {
	t.Helper()
	// Isolate BOTH config roots: tool lists CONCATENATE across global+repo, so
	// without this the developer's own review tools would join the fixture's.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // reports and the approval store
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "feature work")
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(tools), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "config")
	return dir
}

const echoReviewTool = `
[[tools.command]]
category = "review"
name = "Echo"
mode = "capture"
command = '''
printf '# review of <range>\nlooks fine\n'
'''
`

// reviewSvc opens dir with its own note store: the suite runs with notes
// disabled, and a finished commit/branch review is stored as a note.
func reviewSvc(t *testing.T, dir string) *domain.Service {
	t.Helper()
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	return svc
}
