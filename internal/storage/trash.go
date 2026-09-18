package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TrashEntry struct {
	ID            string `json:"id"`
	Epoch         string `json:"epoch"`
	FileCount     int    `json:"fileCount"`
	Bytes         int64  `json:"bytes"`
	QuarantinedAt int64  `json:"quarantinedAt,omitempty"`
	Mode          string `json:"mode,omitempty"`
	Partial       bool   `json:"partial,omitempty"`
}

type TrashRecovery struct {
	ID     string `json:"id,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

type TrashList struct {
	Entries        []TrashEntry    `json:"entries"`
	RecoveryNeeded []TrashRecovery `json:"recoveryNeeded,omitempty"`
}

type ManifestFile struct {
	RelPath string `json:"relPath"`
	Bytes   int64  `json:"bytes"`
	MtimeMs int64  `json:"mtimeMs,omitempty"`
	Mode    uint32 `json:"mode,omitempty"`
	Phase   string `json:"phase,omitempty"`
	Nlink   uint32 `json:"nlink,omitempty"`
}

type Manifest struct {
	ID            string           `json:"id"`
	Epoch         string           `json:"epoch"`
	Mode          string           `json:"mode"`
	QuarantinedAt int64            `json:"quarantinedAt"`
	Status        string           `json:"status"`
	Files         []ManifestFile   `json:"files"`
	Threads       []map[string]any `json:"threads,omitempty"`
	FileCount     int              `json:"fileCount"`
	Bytes         int64            `json:"bytes"`
}

func validTrashID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > maxTrashIDLen {
		return false
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return false
	}
	parts := strings.Split(id, "-")
	if len(parts) != 2 {
		return false
	}
	if _, err := strconv.ParseInt(parts[0], 10, 64); err != nil {
		return false
	}
	if len(parts[1]) < 8 || len(parts[1]) > 32 {
		return false
	}
	for _, c := range parts[1] {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}

func newTrashID(now time.Time, entropy io.Reader) (string, error) {
	if entropy == nil {
		entropy = rand.Reader
	}
	raw := make([]byte, 8)
	if _, err := io.ReadFull(entropy, raw); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%s", now.UnixMilli(), hex.EncodeToString(raw)), nil
}

func ListTrash(codexHome string) (TrashList, error) {
	list := TrashList{Entries: []TrashEntry{}}
	root, err := OpenRoot(codexHome)
	if err != nil {
		if os.IsNotExist(err) {
			return list, nil
		}
		return list, err
	}
	if _, err := os.Lstat(root.TrashDir()); err != nil {
		if os.IsNotExist(err) {
			return list, nil
		}
		return list, err
	}
	info, err := os.Lstat(root.TrashDir())
	if err != nil {
		return list, err
	}
	if !isDirNotReparse(info) {
		list.RecoveryNeeded = append(list.RecoveryNeeded, TrashRecovery{Status: "unreadable", Error: CodePathEscape})
		return list, nil
	}
	dir := root.TrashDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return list, err
	}
	out := make([]TrashEntry, 0)
	for _, entry := range entries {
		name := entry.Name()
		if !validTrashID(name) {
			continue
		}
		item, rec, err := readTrashEntryOrRecovery(root, name)
		if err != nil {
			if rec.Status != "" {
				list.RecoveryNeeded = append(list.RecoveryNeeded, rec)
			}
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].QuarantinedAt != out[j].QuarantinedAt {
			return out[i].QuarantinedAt > out[j].QuarantinedAt
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > maxTrashList {
		out = out[:maxTrashList]
	}
	list.Entries = out
	return list, nil
}

func readTrashEntryOrRecovery(root Root, id string) (TrashEntry, TrashRecovery, error) {
	man, err := loadManifest(root, id)
	if err != nil {
		code := asError(err).Code
		if code == "" {
			code = CodeInvalidTrash
		}
		status := "unreadable"
		if code == CodeMissingTrash {
			status = "incomplete"
		}
		if code == CodeInvalidTrash {
			status = "corrupt"
		}
		return TrashEntry{}, TrashRecovery{ID: id, Status: status, Error: code}, err
	}
	partial := man.Status != "complete"
	for _, file := range man.Files {
		if file.Phase != "" && file.Phase != "quarantined" && file.Phase != "deleted" && file.Phase != "restored" {
			partial = true
		}
		if file.Phase == "planned" || file.Phase == "moving" {
			partial = true
		}
	}
	return TrashEntry{
		ID:            man.ID,
		Epoch:         man.Epoch,
		FileCount:     man.FileCount,
		Bytes:         man.Bytes,
		QuarantinedAt: man.QuarantinedAt,
		Mode:          man.Mode,
		Partial:       partial,
	}, TrashRecovery{}, nil
}

func loadManifest(root Root, id string) (Manifest, error) {
	if !validTrashID(id) {
		return Manifest{}, coded(CodeInvalidTrash, "trash entry id is missing or invalid")
	}
	raw, err := root.readConfinedFile(path.Join(trashDirName, id, manifestName), maxManifestBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, coded(CodeMissingTrash, "trash entry was not found")
		}
		if asError(err).Code == CodePathEscape {
			return Manifest{}, coded(CodeInvalidTrash, "trash manifest is invalid")
		}
		return Manifest{}, err
	}
	if len(raw) > maxManifestBytes {
		return Manifest{}, coded(CodeInvalidTrash, "trash manifest is too large")
	}
	var man Manifest
	if json.Unmarshal(raw, &man) != nil || !validTrashID(man.ID) || man.ID != id {
		return Manifest{}, coded(CodeInvalidTrash, "trash manifest is invalid")
	}
	if man.Mode != ModeQuarantine && man.Mode != ModePermanent {
		man.Mode = ModeQuarantine
	}
	safe := make([]ManifestFile, 0, len(man.Files))
	bytes := int64(0)
	for _, file := range man.Files {
		rel, err := normalizeRel(file.RelPath)
		if err != nil || !archivedPrefix(rel) {
			return Manifest{}, coded(CodeInvalidTrash, "trash manifest path is invalid")
		}
		file.RelPath = rel
		safe = append(safe, file)
		bytes += file.Bytes
	}
	man.Files = safe
	if man.FileCount == 0 {
		man.FileCount = len(safe)
	}
	if man.Bytes == 0 {
		man.Bytes = bytes
	}
	return man, nil
}

func writeManifest(root Root, man Manifest) error {
	raw, err := json.Marshal(man)
	if err != nil {
		return err
	}
	if len(raw) > maxManifestBytes {
		return coded(CodeFSFailed, "trash manifest would be too large")
	}
	return root.writeConfined(path.Join(trashDirName, man.ID, manifestName), raw, 0o600)
}

func createTrashDir(root Root, now time.Time, entropy io.Reader) (id, dir string, err error) {
	if _, err := root.EnsureRealDir(trashDirName); err != nil {
		return "", "", err
	}
	for i := 0; i < 8; i++ {
		id, err = newTrashID(now, entropy)
		if err != nil {
			return "", "", err
		}
		entryRel := path.Join(trashDirName, id)
		dirAbs, err := root.mkdirExclusive(entryRel)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", "", err
		}
		if _, err := root.EnsureRealDir(path.Join(entryRel, filesDirName)); err != nil {
			_ = root.removeTreeConfined(entryRel)
			return "", "", err
		}
		return id, dirAbs, nil
	}
	return "", "", coded(CodeFSFailed, "could not allocate a unique trash id")
}

func pendingRestoreIDs(root Root) (map[string]bool, error) {
	dir := root.TrashDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	out := map[string]bool{}
	for _, entry := range entries {
		if !validTrashID(entry.Name()) {
			continue
		}
		man, err := loadManifest(root, entry.Name())
		if err != nil {
			out[entry.Name()] = true
			continue
		}
		if man.Status == "restoring" || man.Status == "moving" || man.Status == "partial" {
			out[entry.Name()] = true
		}
	}
	return out, nil
}

func RelTrashDir(id string) string {
	return path.Join(trashDirName, id)
}
