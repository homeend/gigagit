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
	case len(a) >= 4 && a[0] == "api" && a[1] == "graphql" && a[2] == "--input":
		os.Exit(mutation(dir, a[3]))
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
	if strings.HasPrefix(name, "snapshot-") && shownWrite(dir) {
		// Only a SUCCESSFUL write GitHub would show — a submitted review, a
		// reply, a resolve — switches to the sent snapshot; a failed send
		// (StartReview … DeleteReview) shows nothing.
		sent := strings.TrimSuffix(name, ".json") + "-sent.json"
		if b2, err2 := os.ReadFile(filepath.Join(dir, sent)); err2 == nil {
			os.Stdout.Write(b2)
			return
		}
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

// mutation answers one write: it appends {"op","variables","failed"} to
// writes.jsonl (what tests assert on), fails with fail-<Op>'s text on stderr
// when that file exists, else prints mutation-<Op>.json or a built-in answer.
func mutation(dir, input string) int {
	b, err := os.ReadFile(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakegh:", err)
		return 1
	}
	var doc struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		fmt.Fprintln(os.Stderr, "fakegh:", err)
		return 1
	}
	op := strings.TrimPrefix(doc.Query, "mutation ")
	if i := strings.IndexAny(op, "({ "); i >= 0 {
		op = op[:i]
	}
	_ = os.MkdirAll(dir, 0o755)
	failMsg, failErr := os.ReadFile(filepath.Join(dir, "fail-"+op))
	line, _ := json.Marshal(map[string]any{"op": op, "variables": doc.Variables, "failed": failErr == nil})
	if f, err := os.OpenFile(filepath.Join(dir, "writes.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.Write(append(line, '\n'))
		f.Close()
	}
	if failErr == nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(string(failMsg)))
		return 1
	}
	if out, err := os.ReadFile(filepath.Join(dir, "mutation-"+op+".json")); err == nil {
		os.Stdout.Write(out)
		return 0
	}
	k := countWrites(dir, op)
	answers := map[string]string{
		"StartReview":  `{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"PRR_new"}}}}`,
		"AddThread":    fmt.Sprintf(`{"data":{"addPullRequestReviewThread":{"thread":{"id":"PRRT_new%d","comments":{"nodes":[{"id":"PRRC_new%d","url":"https://github.com/o/r/pull/7#discussion_new%d"}]}}}}}`, k, k, k),
		"Reply":        fmt.Sprintf(`{"data":{"addPullRequestReviewThreadReply":{"comment":{"id":"PRRC_reply%d","url":"https://github.com/o/r/pull/7#reply%d"}}}}`, k, k),
		"SubmitReview": `{"data":{"submitPullRequestReview":{"pullRequestReview":{"id":"PRR_new","state":"COMMENTED"}}}}`,
		"DeleteReview": `{"data":{"deletePullRequestReview":{"pullRequestReview":{"id":"PRR_new"}}}}`,
		"Resolve":      `{"data":{"resolveReviewThread":{"thread":{"id":"x","isResolved":true}}}}`,
		"Unresolve":    `{"data":{"unresolveReviewThread":{"thread":{"id":"x","isResolved":false}}}}`,
	}
	out, ok := answers[op]
	if !ok {
		fmt.Fprintln(os.Stderr, "fakegh: unsupported mutation:", op)
		return 2
	}
	fmt.Print(out)
	return 0
}

// shownWrite reports a recorded, successful SubmitReview / Reply / Resolve /
// Unresolve.
func shownWrite(dir string) bool {
	b, _ := os.ReadFile(filepath.Join(dir, "writes.jsonl"))
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var w struct {
			Op     string `json:"op"`
			Failed bool   `json:"failed"`
		}
		if json.Unmarshal([]byte(line), &w) == nil && !w.Failed {
			switch w.Op {
			case "SubmitReview", "Reply", "Resolve", "Unresolve":
				return true
			}
		}
	}
	return false
}

// countWrites is how many times op has been recorded (this call included):
// the k in a created id, so two threads get two ids.
func countWrites(dir, op string) int {
	b, _ := os.ReadFile(filepath.Join(dir, "writes.jsonl"))
	return strings.Count(string(b), `"op":"`+op+`"`)
}
