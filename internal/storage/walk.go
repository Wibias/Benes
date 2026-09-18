package storage

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type classifiedFile struct {
	RelPath string
	AbsPath string
	Bytes   int64
	MtimeMs int64
	Mode    uint32
	ID      fileID
	Bucket  BucketKey
}

type walkResult struct {
	Files     []classifiedFile
	Truncated bool
}

func walkHome(root Root) (walkResult, error) {
	entries, err := os.ReadDir(root.Abs)
	if err != nil {
		if os.IsNotExist(err) {
			return walkResult{}, nil
		}
		return walkResult{}, err
	}
	out := walkResult{Files: make([]classifiedFile, 0, 64)}
	for _, entry := range entries {
		if len(out.Files) >= maxWalkFiles {
			out.Truncated = true
			break
		}
		name := entry.Name()
		if isTrashName(name) {
			continue
		}
		full := filepath.Join(root.Abs, name)
		info, err := os.Lstat(full)
		if err != nil {
			continue
		}
		if isDirNotReparse(info) {
			key := dirBuckets[name]
			if key == "" {
				key = BucketOther
			}
			walkDir(root, full, filepath.ToSlash(name), key, &out)
			continue
		}
		if info.IsDir() {
			continue
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		key := BucketOther
		if stateDBFile.MatchString(name) {
			key = BucketStateDB
		} else if logsDBFile.MatchString(name) {
			key = BucketLogsDB
		}
		out.Files = append(out.Files, classifiedFile{
			RelPath: filepath.ToSlash(name),
			AbsPath: full,
			Bytes:   info.Size(),
			MtimeMs: info.ModTime().UnixMilli(),
			Mode:    fileModeBits(info),
			ID:      readFileID(full, info),
			Bucket:  key,
		})
	}
	return out, nil
}

func walkDir(root Root, dir, relPrefix string, key BucketKey, out *walkResult) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if len(out.Files) >= maxWalkFiles {
			out.Truncated = true
			return
		}
		name := entry.Name()
		full := filepath.Join(dir, name)
		relPath := path.Join(relPrefix, name)
		if err := root.ensureInside(full, false); err != nil {
			continue
		}
		info, err := os.Lstat(full)
		if err != nil {
			continue
		}
		if isDirNotReparse(info) {
			walkDir(root, full, relPath, key, out)
			continue
		}
		if info.IsDir() {
			continue
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		out.Files = append(out.Files, classifiedFile{
			RelPath: relPath,
			AbsPath: full,
			Bytes:   info.Size(),
			MtimeMs: info.ModTime().UnixMilli(),
			Mode:    fileModeBits(info),
			ID:      readFileID(full, info),
			Bucket:  key,
		})
	}
}

func filesForBucket(files []classifiedFile, key BucketKey) []classifiedFile {
	out := make([]classifiedFile, 0)
	for _, file := range files {
		if file.Bucket == key {
			out = append(out, file)
		}
	}
	return out
}

func sortOldest(files []classifiedFile) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].MtimeMs != files[j].MtimeMs {
			return files[i].MtimeMs < files[j].MtimeMs
		}
		return files[i].RelPath < files[j].RelPath
	})
}

func sortLargest(files []classifiedFile) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].Bytes != files[j].Bytes {
			return files[i].Bytes > files[j].Bytes
		}
		return files[i].RelPath < files[j].RelPath
	})
}

func physicalGroups(files []classifiedFile) map[fileID][]int {
	groups := map[fileID][]int{}
	for i, file := range files {
		if file.ID.OK && file.ID.Nlink > 1 {
			groups[file.ID] = append(groups[file.ID], i)
		}
	}
	return groups
}

func archivedPrefix(rel string) bool {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	return rel == "archived_sessions" || strings.HasPrefix(rel, "archived_sessions/")
}
