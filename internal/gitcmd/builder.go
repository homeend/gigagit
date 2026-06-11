// Package gitcmd builds git argument vectors fluently. Global options (-C, -c)
// are prepended so they precede the subcommand, as git requires.
package gitcmd

// Builder accumulates a git argument vector.
type Builder struct {
	args []string
}

// New starts a builder for the given git subcommand (e.g. "status").
func New(subcommand string) *Builder {
	return &Builder{args: []string{subcommand}}
}

// Arg appends positional arguments/flags.
func (b *Builder) Arg(a ...string) *Builder {
	b.args = append(b.args, a...)
	return b
}

// ArgIf appends arguments only when cond is true.
func (b *Builder) ArgIf(cond bool, a ...string) *Builder {
	if cond {
		b.args = append(b.args, a...)
	}
	return b
}

// Config prepends "-c kv" before the subcommand.
func (b *Builder) Config(kv string) *Builder {
	b.args = append([]string{"-c", kv}, b.args...)
	return b
}

// Dir prepends "-C path" before the subcommand, making git operate in path.
func (b *Builder) Dir(path string) *Builder {
	b.args = append([]string{"-C", path}, b.args...)
	return b
}

// ToArgv returns the assembled argument vector.
func (b *Builder) ToArgv() []string {
	out := make([]string, len(b.args))
	copy(out, b.args)
	return out
}
