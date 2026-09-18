//go:build unix

package storage

import (
	"errors"
	"os"
	"syscall"
)

func openNoFollow(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, errPathEscape
		}
		return nil, err
	}
	return f, nil
}
