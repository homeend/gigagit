package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
)

// cmdCommitReword implements `gg commit reword <commit> -m <message>`. Parsing
// is order-independent (the commit positional may precede or follow -m),
// mirroring cmdCheckout. v1 requires -m (no editor).
func cmdCommitReword(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	commit := ""
	msg := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-m" || a == "--message":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "usage: gg commit reword <commit> -m <message>")
				return 2
			}
			msg = args[i+1]
			i++
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(stderr, "commit reword: unknown flag %q\n", a)
			return 2
		default:
			if commit != "" {
				fmt.Fprintln(stderr, "commit reword: too many arguments (expected one <commit>)")
				return 2
			}
			commit = a
		}
	}
	if commit == "" || msg == "" {
		fmt.Fprintln(stderr, "usage: gg commit reword <commit> -m <message>")
		return 2
	}
	ggBin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, "commit reword:", err)
		return 1
	}
	res, err := runOperation(context.Background(), svc,
		engine.Reword{Commit: commit, NewMsg: msg, GGBin: ggBin}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
