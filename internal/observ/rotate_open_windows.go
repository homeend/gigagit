//go:build windows

package observ

import (
	"os"
	"syscall"
)

// openAppend opens path for appending, creating it as needed — with
// FILE_SHARE_DELETE, which os.OpenFile leaves out. Windows refuses to rename a
// file any handle holds without it, so a second gg process (the TUI beside gg
// web) would block every weekly rollover for as long as both run. With it,
// a rollover behaves as on Unix: the rename succeeds and the other process
// keeps appending to the archive until its own first write of the new week.
func openAppend(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := syscall.CreateFile(p,
		syscall.FILE_APPEND_DATA|syscall.SYNCHRONIZE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
