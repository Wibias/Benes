//go:build unix

package usageledger

import "os"

func replaceFile(tmp, dest string) error {
	return os.Rename(tmp, dest)
}
