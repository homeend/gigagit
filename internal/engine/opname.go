package engine

import (
	"fmt"
	"strings"
)

// OpName returns a stable, human-readable name for an operation (its type
// name without the package prefix, e.g. "SmartPull"). Frontends use it to
// label timing spans.
func OpName(op Operation) string {
	name := fmt.Sprintf("%T", op)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}
