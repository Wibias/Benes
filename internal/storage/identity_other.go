//go:build !windows && !unix

package storage

import "os"

type fileID struct {
	Vol   uint64
	Idx   uint64
	Nlink uint32
	OK    bool
}

func readFileID(path string, info os.FileInfo) fileID {
	_ = path
	_ = info
	return fileID{}
}

func (id fileID) Equal(other fileID) bool {
	return id.OK && other.OK && id.Vol == other.Vol && id.Idx == other.Idx
}
