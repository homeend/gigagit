package gitwatch

import "testing"

func TestV9fsMagicConstant(t *testing.T) {
	// WSL2 drvfs (/mnt/*) mounts report the 9P filesystem magic. Pin it so the
	// drvfs gate can't silently drift.
	if V9fsMagic != 0x01021997 {
		t.Fatalf("V9fsMagic = %#x, want 0x01021997", V9fsMagic)
	}
}

func TestSupportedOnTempDir(t *testing.T) {
	// t.TempDir() is /tmp (ext4 here), a normal local fs → watching is viable.
	if !Supported(t.TempDir()) {
		t.Fatal("Supported(tempdir) = false, want true on a local fs")
	}
}
