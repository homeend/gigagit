package e2e

import (
	"net/http/cgi"
	"net/http/httptest"
	"os/exec"
	"testing"
)

// startGitServer serves every repo under root over git's HTTP smart protocol
// (upload-pack and receive-pack), by hosting `git http-backend` — git's own
// CGI server — in-process. Each caller gets its own listener on an ephemeral
// port; the server stops via t.Cleanup.
//
// Repos must opt into anonymous push with `http.receivepack = true`.
func startGitServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git not found in PATH: %v", err)
	}
	h := &cgi.Handler{
		Path: gitPath,
		Args: []string{"http-backend"},
		Env: []string{
			"GIT_PROJECT_ROOT=" + root,
			"GIT_HTTP_EXPORT_ALL=1",
		},
		// The git http-backend CGI process needs the git identity env vars
		// that TestMain pins (GIT_CONFIG_NOSYSTEM, GIT_CONFIG_GLOBAL, etc.)
		// and PATH to locate git itself. net/http/cgi does not inherit the
		// process environment by default, so we name them explicitly here.
		InheritEnv: []string{
			"PATH",
			"GIT_CONFIG_NOSYSTEM",
			"GIT_CONFIG_GLOBAL",
			"GIT_AUTHOR_NAME",
			"GIT_AUTHOR_EMAIL",
			"GIT_COMMITTER_NAME",
			"GIT_COMMITTER_EMAIL",
		},
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}
