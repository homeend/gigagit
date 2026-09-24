package tui

// maxOpenFiles is how many files one worktree keeps open (spec ruling 4).
const maxOpenFiles = 20

// openFilesReg is the open-files list: per worktree, the documents the user
// or an agent opened, most recently shown first. A document leaves it only
// when it is closed on purpose (esc on it, x in the switcher) or evicted over
// the cap — every other teardown leaves it in the background.
type openFilesReg struct {
	byWT map[string][]*openFile
}

// list is wt's open files, most recently shown first. A nil registry is empty.
func (r *openFilesReg) list(wt string) []*openFile {
	if r == nil {
		return nil
	}
	return r.byWT[wt]
}

// find is wt's open document with key, or nil.
func (r *openFilesReg) find(wt, key string) *openFile {
	for _, d := range r.list(wt) {
		if d.key() == key {
			return d
		}
	}
	return nil
}

// touch puts d first in wt's list, adding it when new. Over the cap it drops
// the least recently shown document that is not on screen (shown reports
// that) and returns it; nil when nothing was dropped.
func (r *openFilesReg) touch(wt string, d *openFile, shown func(*openFile) bool) (evicted *openFile) {
	if r.byWT == nil {
		r.byWT = map[string][]*openFile{}
	}
	l := []*openFile{d}
	for _, e := range r.byWT[wt] {
		if e != d {
			l = append(l, e)
		}
	}
	if len(l) > maxOpenFiles {
		for i := len(l) - 1; i > 0; i-- {
			if !shown(l[i]) {
				evicted = l[i]
				l = append(l[:i], l[i+1:]...)
				break
			}
		}
	}
	r.byWT[wt] = l
	return evicted
}

// remove drops d from wt's list.
func (r *openFilesReg) remove(wt string, d *openFile) {
	if r == nil {
		return
	}
	l := r.byWT[wt][:0:0]
	for _, e := range r.byWT[wt] {
		if e != d {
			l = append(l, e)
		}
	}
	r.byWT[wt] = l
}
