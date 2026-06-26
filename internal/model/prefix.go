package model

import "time"

// Prefix is a reusable branch-name skeleton (possibly templated). Its identity
// is its Value (slugged into ID); Scope is implied by which store holds it and
// is set on List/Add. Reuses ProfileScope (global vs repo).
type Prefix struct {
	ID      string       `toml:"id"`
	Value   string       `toml:"value"`
	Scope   ProfileScope `toml:"-"`
	Created time.Time    `toml:"created"`
}
