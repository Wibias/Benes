//go:build darwin

package storage

import (
	"errors"

	"golang.org/x/sys/unix"
)

func renameNoReplace(oldpath, newpath string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, oldpath, unix.AT_FDCWD, newpath, unix.RENAME_EXCL)
}

func renameAlreadyExists(err error) bool {
	return errors.Is(err, unix.EEXIST)
}
