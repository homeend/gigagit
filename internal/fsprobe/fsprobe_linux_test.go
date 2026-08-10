package fsprobe

import "testing"

func TestForeignTypeTable(t *testing.T) {
	for _, ft := range []int64{v9fsMagic, cifsMagic, smb2Magic, nfsMagic, fuseMagic, afsMagic} {
		if !foreignType(ft) {
			t.Errorf("foreignType(%#x) = false, want true", ft)
		}
	}
	local := []int64{
		0xEF53,     // ext2/3/4
		0x01021994, // tmpfs
		0x9123683E, // btrfs
		0x58465342, // xfs
		0x2FC12FC1, // zfs
		0x137D,     // ext (ancient; arbitrary non-member)
	}
	for _, ft := range local {
		if foreignType(ft) {
			t.Errorf("foreignType(%#x) = true, want false", ft)
		}
	}
}

// TestForeignLocalDir pins the negative on a real local path: the test tmp dir
// (ext4/tmpfs everywhere gg develops) must never be flagged.
func TestForeignLocalDir(t *testing.T) {
	if Foreign(t.TempDir()) {
		t.Fatal("Foreign(t.TempDir()) = true, want false on a local filesystem")
	}
}

// TestForeignMissingPath pins fail-open: an unreachable path yields no warning
// rather than a spurious one.
func TestForeignMissingPath(t *testing.T) {
	if Foreign("/nonexistent/definitely/not/here") {
		t.Fatal("Foreign(missing path) = true, want false (fail open)")
	}
}
