package e2e

import (
	"bytes"
	"io"

	"github.com/gigagit/gg/internal/cli"
)

// Runner executes one gg command against workdir and returns its exit code.
// CLIRunner is the v1 implementation; an MCPRunner mapping the same argv onto
// MCP tool calls is planned for M3 — scenario files stay unchanged.
type Runner interface {
	Run(workdir string, argv []string, stdout, stderr io.Writer) int
}

// CLIRunner drives the CLI frontend in-process: the full CLI→engine→real-git
// stack. stdin is an empty non-TTY reader, so any engine decision a scenario
// does not pre-answer via flags fails deterministically.
type CLIRunner struct{}

func (CLIRunner) Run(workdir string, argv []string, stdout, stderr io.Writer) int {
	return cli.Run(workdir, argv, bytes.NewReader(nil), stdout, stderr, "")
}
