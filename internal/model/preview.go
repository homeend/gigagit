package model

import "time"

// MergePreview is one saved "what would Source bring into Target" pair. It
// stores branch NAMES, never hashes: every open resolves the current tips,
// which is what makes a saved preview follow the branches as they move. Plain
// data (like Bookmark), persisted by internal/preview as TOML.
type MergePreview struct {
	ID      string    `toml:"id"`     // derived from (Source, Target); direction-sensitive
	Source  string    `toml:"source"` // e.g. "feat/login" or "origin/feat/login"
	Target  string    `toml:"target"` // e.g. "main" or "origin/main"
	Label   string    `toml:"label"`  // human label; defaults to "<source> → <target>"
	Created time.Time `toml:"created"`
}

// DefaultLabel is the label a preview gets when the user gives none.
func (p MergePreview) DefaultLabel() string { return p.Source + " → " + p.Target }
