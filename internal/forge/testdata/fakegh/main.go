// fakegh stands in for the gh CLI in tests: it maps an invocation to a canned
// JSON file under $GG_FAKEGH_DIR, else <cwd>/.git/fakegh. A missing fixture
// dir or file exits 1 — exactly how a box without a usable gh behaves.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	dir := os.Getenv("GG_FAKEGH_DIR")
	if dir == "" {
		dir = filepath.Join(".git", "fakegh")
	}
	a := os.Args[1:]
	name := ""
	switch {
	case len(a) >= 2 && a[0] == "pr" && a[1] == "list":
		name = "pr-list.json"
	case len(a) >= 3 && a[0] == "pr" && a[1] == "view":
		name = "pr-view-" + a[2] + ".json"
	case len(a) >= 2 && a[0] == "repo" && a[1] == "view":
		name = "repo-view.json"
	case len(a) >= 2 && a[0] == "api" && a[1] == "graphql":
		for _, s := range a {
			if n, ok := strings.CutPrefix(s, "number="); ok {
				name = "threads-" + n + ".json"
			}
		}
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "fakegh: unsupported invocation:", strings.Join(a, " "))
		os.Exit(2)
	}
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		if strings.HasPrefix(name, "pr-view-") {
			fmt.Fprintln(os.Stderr, "GraphQL: Could not resolve to a PullRequest with the number of "+a[2]+".")
		} else {
			fmt.Fprintln(os.Stderr, "fakegh:", err)
		}
		os.Exit(1)
	}
	os.Stdout.Write(b)
}
