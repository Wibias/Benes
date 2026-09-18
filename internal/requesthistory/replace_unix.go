//go:build !windows

package requesthistory

import "os"

func replaceFile(tmp, dest string) error {
	return os.Rename(tmp, dest)
}
