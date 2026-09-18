//go:build unix

package storage

import (
	"os"
	"syscall"
)

type fileID struct {
	Vol   uint64
	Idx   uint64
	Nlink uint32
	OK    bool
}

func readFileID(path string, info os.FileInfo) fileID {
	_ = path
	if info == nil || isReparse(info) {
		return fileID{}
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return fileID{}
	}
	return fileID{
		Vol:   uint64(st.Dev),
		Idx:   uint64(st.Ino),
		Nlink: uint32(st.Nlink),
		OK:    true,
	}
}

func (id fileID) Equal(other fileID) bool {
	return id.OK && other.OK && id.Vol == other.Vol && id.Idx == other.Idx
}
