//go:build !linux

package gitwatch

// V9fsMagic mirrors the Linux constant so cross-platform tests compile; it is
// unused off Linux.
const V9fsMagic = 0x01021997

// Supported reports whether file-watching is viable. On non-Linux platforms
// (macOS kqueue, Windows ReadDirectoryChangesW) the drvfs problem does not
// apply, so watching is always considered viable.
func Supported(commonDir string) bool { return true }
