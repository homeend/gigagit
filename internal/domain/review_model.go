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
// model quoted as one argument. A tool with neither refuses the model, and
// so does one whose command would hand the appended flag to something else
// (template.AppendBlocker).
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
	resolved = strings.TrimRight(resolved, " \t\r\n")
	// After a pipe, a list operator, a new line or a comment the flag would
	// reach another program (or none) and the review would run on the
	// default model: refuse, naming the slot that works.
	if sep := template.AppendBlocker(resolved); sep != "" {
		// <model:FLAG> joins with a space, which every agent's parser takes
		// (Junie's "--model=" too); "--model=<model>" would leave a bare
		// --model= on every run without a model.
		slot := "<model:" + strings.TrimRight(strings.TrimSpace(flag), "=") + ">"
		return "", fmt.Errorf("%w: review tool %q: gg would add the model after %s in its command, where %s would not get it — put %s right after the agent's own arguments",
			ErrNoModelSupport, tc.Name, describeSep(sep), ToolAgentID(tc), slot)
	}
	return resolved + " " + flag + template.QuoteArg(ctx.Model), nil
}

// ReviewTakesModel reports whether tc can take `gg review --model` — the
// model answer ResolveReviewCommand gives (gg review --tools prints it).
func ReviewTakesModel(tc config.ToolCommand) bool {
	_, err := ResolveReviewCommand(tc, template.CmdCtx{Model: "m"})
	return !errors.Is(err, ErrNoModelSupport) // another error is the tool's own
}

// describeSep names an AppendBlocker result in a refusal: an operator is
// quoted, "a line break" reads as it is.
func describeSep(sep string) string {
	if strings.Contains(sep, " ") {
		return sep
	}
	return "`" + sep + "`"
}
