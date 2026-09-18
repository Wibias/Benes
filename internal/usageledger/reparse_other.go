//go:build !windows

package usageledger

import "os"

func runtimeVolumeMismatch(root, candidate string) bool {
	_ = root
	_ = candidate
	return false
}

func windowsReparse(info os.FileInfo) bool {
	_ = info
	return false
}
