//go:build !windows && !unix

package usageledger

import "os"

func replaceFile(tmp, dest string) error {
	_ = os.Remove(dest)
	return os.Rename(tmp, dest)
}
