package prcache

import (
	"errors"
	"syscall"
)

// errSharingViolation is ERROR_SHARING_VIOLATION; syscall names no constant.
const errSharingViolation syscall.Errno = 32

func sharingViolation(err error) bool {
	var e syscall.Errno
	return errors.As(err, &e) && e == errSharingViolation
}
