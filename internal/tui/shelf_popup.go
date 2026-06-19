package tui

import "github.com/gigagit/gg/internal/model"

// shelfPopup is the centered shelf quick-switcher (G): a type-to-filter list of
// the repo's shelved files. Mirrors bookmarkPopup. Filled out in the popup task.
type shelfPopup struct {
	items     []model.ShelfEntry
	rows      []string // e.Origin.Display(), parallel to items
	sel       int
	filter    string
	filtering bool
	markID    string
	mode      dispMode
	hscroll   int

	compareRef   *model.FileRef // compare mode: enter diffs compareRef (left) vs the picked entry (right)
	compareLabel string
}
