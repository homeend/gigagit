// Command ggfake stands in for an AI agent in e2e scenarios: `ggfake review`
// prints the canned review document named by $GGFAKE_REVIEW (TestMain points
// it at e2e/fixtures/review.json).
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "review" {
		fmt.Fprintln(os.Stderr, "usage: ggfake review")
		os.Exit(2)
	}
	b, err := os.ReadFile(os.Getenv("GGFAKE_REVIEW"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(b)
}
