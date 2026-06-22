package model

import "time"

// ProfileScope distinguishes a global app-profile (available in every repo)
// from a repo-specific one (only the repo it was created in).
type ProfileScope int

const (
	ProfileScopeGlobal ProfileScope = iota
	ProfileScopeRepo
)

func (s ProfileScope) String() string {
	if s == ProfileScopeRepo {
		return "repo"
	}
	return "global"
}

// Profile is a named git-identity preset. ID is derived from Name (slug);
// renaming yields a new ID (remove-then-add).
type Profile struct {
	ID       string       `toml:"id"`
	Name     string       `toml:"name"`
	GitName  string       `toml:"git_name"`
	GitEmail string       `toml:"git_email"`
	Scope    ProfileScope `toml:"-"` // implied by which store holds it; set on List
	Created  time.Time    `toml:"created"`
}

// Identity is the current git user identity, with global and repo-local
// values kept distinct (each with a "set?" flag) plus the effective merged
// value git would actually record in a commit.
type Identity struct {
	GlobalName, GlobalEmail       string
	GlobalSet                     bool
	LocalName, LocalEmail         string
	LocalSet                      bool
	EffectiveName, EffectiveEmail string
}
