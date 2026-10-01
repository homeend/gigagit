// Package texttmpl is gigagit's writable registry of text templates: titled,
// multi-line texts with <…> tokens. Two scopes — global (every repo) and
// repo-specific — each a separate file-backed store (the prefix precedent).
package texttmpl

import (
	"errors"

	"github.com/homeend/gigagit/internal/model"
)

var (
	// ErrNotFound is returned by Get/Update/Remove for an unknown id.
	ErrNotFound = errors.New("text template: not found")
	// ErrDuplicate is returned when a title's id is already taken in the scope.
	ErrDuplicate = errors.New("text template: a template with this title already exists")
	// ErrNoID is returned for a title with no letter or digit to build an id from.
	ErrNoID = errors.New("text template: the title needs a letter or a digit")
)

// Store persists text templates for one scope (atomic rewrite, last-writer-wins).
type Store interface {
	Add(t model.TextTemplate) (model.TextTemplate, error)
	Get(id string) (model.TextTemplate, error)
	List() ([]model.TextTemplate, error)
	// Update replaces the template stored under id. The id follows the title;
	// Created is kept.
	Update(id string, t model.TextTemplate) (model.TextTemplate, error)
	Remove(id string) error
}
