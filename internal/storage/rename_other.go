//go:build !linux && !darwin && !windows

package storage

import (
	"os"
)

func renameNoReplace(oldpath, newpath string) error {
	if err := os.Link(oldpath, newpath); err != nil {
		return err
	}
	if err := os.Remove(oldpath); err != nil {
		_ = os.Remove(newpath)
		return err
	}
	return nil
}

func renameAlreadyExists(err error) bool {
	return err != nil && os.IsExist(err)
}
