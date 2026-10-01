package model

import "time"

// TextTemplate is a reusable, titled piece of multi-line text with <…> tokens
// (the branch-prefix grammar). Its identity is its Title (slugged into ID);
// Scope is implied by which store holds it and is set on List/Add.
type TextTemplate struct {
	ID      string       `toml:"id"`
	Title   string       `toml:"title"`
	Body    string       `toml:"body"`
	Scope   ProfileScope `toml:"-"`
	Created time.Time    `toml:"created"`
}
