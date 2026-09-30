package promptstate

import (
	"crypto/sha256"
	"encoding/hex"
)

// ToolUpdateID is the stored form of a tool-template offer key (a hash keeps
// the file printable: offer keys hold NUL separators).
func ToolUpdateID(offerKey string) string {
	sum := sha256.Sum256([]byte(offerKey))
	return hex.EncodeToString(sum[:])
}

// DeclinedToolUpdates is the set of ToolUpdateID values answered "Keep mine"
// in the tool-template review. Machine-global, like the config files.
func (fs *FileStore) DeclinedToolUpdates() map[string]bool {
	return toSet(fs.read().DeclinedToolUpdates)
}

// DeclineToolUpdate records one "Keep mine" (idempotent) and persists.
func (fs *FileStore) DeclineToolUpdate(offerKey string) error {
	r := fs.read()
	id := ToolUpdateID(offerKey)
	if toSet(r.DeclinedToolUpdates)[id] {
		return nil
	}
	r.DeclinedToolUpdates = append(r.DeclinedToolUpdates, id)
	return fs.write(r)
}
