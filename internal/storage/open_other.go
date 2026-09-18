//go:build !unix && !windows

package storage

import (
	"os"
)

func openNoFollow(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if isReparse(info) {
		return nil, errPathEscape
	}
	return os.Open(path)
}
