package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/gigagit/gg/internal/app"
)

func main() {
	var (
		dumpPath = flag.String("debug-dump", "", "write a debug dump JSON file to this path")
		trace    = flag.Bool("trace", false, "enable verbose timing trace to stderr")
	)
	flag.Parse()

	opts := app.Options{WorkDir: ".", Stdout: os.Stdout, DumpPath: *dumpPath}
	if *trace || os.Getenv("GG_TRACE") == "1" {
		opts.Trace = os.Stderr
	}
	if err := app.Inspect(context.Background(), opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
