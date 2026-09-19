package forge

import "testing"

func TestRepoSlug(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"git@github.com:homeend/gigagit.git":           "github.com/homeend/gigagit",
		"ssh://git@github.com/homeend/gigagit.git":     "github.com/homeend/gigagit",
		"ssh://git@github.com:22/homeend/gigagit":      "github.com/homeend/gigagit",
		"https://github.com/homeend/gigagit.git":       "github.com/homeend/gigagit",
		"https://user:tok@github.com/Homeend/GigaGit/": "github.com/homeend/gigagit",
		"/srv/git/repo.git":                            "",
		"":                                             "",
	} {
		if got := RepoSlug(in); got != want {
			t.Errorf("RepoSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
