package domain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/template"
)

// ErrNoModelSupport: `gg review --model` on a tool that cannot take a model —
// neither a built-in agent with a known model flag nor a <model> slot.
var ErrNoModelSupport = errors.New("no model support")

// ResolveReviewCommand resolves a review tool's command for ctx. With
// ctx.Model set, a command holding <model> gets it there; otherwise the
// built-in agent's model flag (exttool.ModelFlagFor) is appended with the
// model quoted as one argument. A tool with neither refuses the model.
func ResolveReviewCommand(tc config.ToolCommand, ctx template.CmdCtx) (string, error) {
	resolved, err := template.ResolveCommand(tc.Command, nil, ctx)
	if err != nil {
		return "", err
	}
	if ctx.Model == "" || template.HasModelSlot(tc.Command) {
		return resolved, nil
	}
	flag := exttool.ModelFlagFor(ToolAgentID(tc))
	if flag == "" {
		return "", fmt.Errorf("%w: review tool %q cannot take a model — add <model> to its command", ErrNoModelSupport, tc.Name)
	}
	// A multi-line config command ends in a newline: the flag must join its
	// last line, or the shell runs it as a command of its own.
	return strings.TrimRight(resolved, " \t\r\n") + " " + flag + template.QuoteArg(ctx.Model), nil
}
