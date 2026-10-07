// fakegh stands in for the gh CLI in tests: it maps an invocation to a canned
// JSON file under $GG_FAKEGH_DIR, else <cwd>/.git/fakegh. A missing fixture
// dir or file exits 1 — exactly how a box without a usable gh behaves.
package main

import (
	"encoding/json"
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
		// A search always carries --search; detection and the open listing
		// never do.
		for _, s := range a {
			if s == "--search" {
				name = "pr-search.json"
			}
		}
	case len(a) >= 3 && a[0] == "pr" && a[1] == "view":
		name = "pr-view-" + a[2] + ".json"
	case len(a) >= 2 && a[0] == "repo" && a[1] == "view":
		name = "repo-view.json"
	case len(a) >= 2 && a[0] == "api" && a[1] == "graphql":
		prefix := "threads-"
		for _, s := range a {
			if strings.HasPrefix(s, "query=query PRSnapshot") {
				prefix = "snapshot-"
			}
		}
		for _, s := range a {
			if n, ok := strings.CutPrefix(s, "number="); ok {
				name = prefix + n + ".json"
			}
		}
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "fakegh: unsupported invocation:", strings.Join(a, " "))
		os.Exit(2)
	}
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil && strings.HasPrefix(name, "snapshot-") {
		// Older fixture sets seed pr-view + threads only: compose the
		// combined read from them.
		b, err = composeSnapshot(dir, strings.TrimSuffix(strings.TrimPrefix(name, "snapshot-"), ".json"))
	}
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

// composeSnapshot builds the combined read from the two older fixtures: the
// threads document, with the pr-view fields merged into its pullRequest node.
func composeSnapshot(dir, n string) ([]byte, error) {
	tb, err := os.ReadFile(filepath.Join(dir, "threads-"+n+".json"))
	if err != nil {
		return nil, err
	}
	pb, err := os.ReadFile(filepath.Join(dir, "pr-view-"+n+".json"))
	if err != nil {
		return nil, err
	}
	var doc, pr map[string]any
	if err := json.Unmarshal(tb, &doc); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(pb, &pr); err != nil {
		return nil, err
	}
	data, _ := doc["data"].(map[string]any)
	repo, _ := data["repository"].(map[string]any)
	node, ok := repo["pullRequest"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("threads-%s.json has no pullRequest node", n)
	}
	for k, v := range pr {
		node[k] = v
	}
	return json.Marshal(doc)
}
