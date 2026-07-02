package model

// GitConfigRow is one key of the git-config explorer: the catalog key plus
// its explicitly-set local and global values. Unset scopes render an
// explicit "(unset)" in the UI — the zero value here IS "unset", which is
// why the Set flags exist (a key set to the empty string is still set).
type GitConfigRow struct {
	Key         string // display form: catalog camelCase when known, else as-set
	LocalValue  string
	LocalSet    bool
	GlobalValue string
	GlobalSet   bool
}
