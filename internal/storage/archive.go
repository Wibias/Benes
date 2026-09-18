package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

type Candidate struct {
	RelPath          string   `json:"relPath"`
	Bytes            int64    `json:"bytes"`
	PhysicalRelPaths []string `json:"physicalRelPaths,omitempty"`
	AbsPath          string   `json:"-"`
	MtimeMs          int64    `json:"-"`
	Mode             uint32   `json:"-"`
	ID               fileID   `json:"-"`
}

type Preview struct {
	Percent    int         `json:"percent,omitempty"`
	Count      int         `json:"count"`
	Bytes      int64       `json:"bytes"`
	Digest     string      `json:"digest"`
	Candidates []Candidate `json:"candidates"`
	Truncated  bool        `json:"truncated,omitempty"`
	WalkTrunc  bool        `json:"-"`
}

func listArchived(root Root) ([]classifiedFile, bool, error) {
	walked, err := walkHome(root)
	if err != nil {
		return nil, false, err
	}
	files := filesForBucket(walked.Files, BucketArchivedSessions)
	return files, walked.Truncated, nil
}

func selectOldestPercent(files []classifiedFile, percent int) []classifiedFile {
	if percent < 1 || percent > 100 || len(files) == 0 {
		return nil
	}
	ordered := append([]classifiedFile(nil), files...)
	sortOldest(ordered)
	n := (len(ordered)*percent + 99) / 100
	if n < 1 {
		n = 1
	}
	if n > len(ordered) {
		n = len(ordered)
	}
	return expandHardlinkUnits(ordered[:n], ordered)
}

func selectReduceToBytes(files []classifiedFile, target int64) []classifiedFile {
	if target < 0 || len(files) == 0 {
		return nil
	}
	ordered := append([]classifiedFile(nil), files...)
	sortOldest(ordered)
	total := int64(0)
	seen := map[fileID]int64{}
	for _, file := range ordered {
		if file.ID.OK {
			if _, ok := seen[file.ID]; ok {
				continue
			}
			seen[file.ID] = file.Bytes
			total += file.Bytes
			continue
		}
		total += file.Bytes
	}
	if total <= target {
		return nil
	}
	var picked []classifiedFile
	remaining := total
	used := map[fileID]bool{}
	for _, file := range ordered {
		if remaining <= target {
			break
		}
		if file.ID.OK && used[file.ID] {
			continue
		}
		picked = append(picked, file)
		if file.ID.OK {
			used[file.ID] = true
			remaining -= file.Bytes
		} else {
			remaining -= file.Bytes
		}
	}
	return expandHardlinkUnits(picked, ordered)
}

func expandHardlinkUnits(picked, universe []classifiedFile) []classifiedFile {
	need := map[fileID]bool{}
	for _, file := range picked {
		if file.ID.OK && file.ID.Nlink > 1 {
			need[file.ID] = true
		}
	}
	if len(need) == 0 {
		out := append([]classifiedFile(nil), picked...)
		sortOldest(out)
		return out
	}
	seen := map[string]bool{}
	var out []classifiedFile
	for _, file := range picked {
		if seen[file.RelPath] {
			continue
		}
		seen[file.RelPath] = true
		out = append(out, file)
	}
	for _, file := range universe {
		if !file.ID.OK || !need[file.ID] || seen[file.RelPath] {
			continue
		}
		seen[file.RelPath] = true
		out = append(out, file)
	}
	sortOldest(out)
	return out
}

func toCandidates(files []classifiedFile) []Candidate {
	byID := map[fileID][]string{}
	for _, file := range files {
		if file.ID.OK && file.ID.Nlink > 1 {
			byID[file.ID] = append(byID[file.ID], file.RelPath)
		}
	}
	for id := range byID {
		sort.Strings(byID[id])
	}
	out := make([]Candidate, 0, len(files))
	for _, file := range files {
		c := Candidate{
			RelPath: file.RelPath,
			Bytes:   file.Bytes,
			AbsPath: file.AbsPath,
			MtimeMs: file.MtimeMs,
			Mode:    file.Mode,
			ID:      file.ID,
		}
		if aliases := byID[file.ID]; len(aliases) > 1 {
			c.PhysicalRelPaths = aliases
		}
		out = append(out, c)
	}
	return out
}

func candidateBytes(files []classifiedFile) int64 {
	total := int64(0)
	seen := map[fileID]bool{}
	for _, file := range files {
		if file.ID.OK {
			if seen[file.ID] {
				continue
			}
			seen[file.ID] = true
		}
		total += file.Bytes
	}
	return total
}

func planDigest(kind string, files []classifiedFile) string {
	h := sha256.New()
	fmt.Fprintf(h, "benes-storage-preview-v1\n%s\n", kind)
	ordered := append([]classifiedFile(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].RelPath < ordered[j].RelPath })
	for _, file := range ordered {
		fmt.Fprintf(h, "%s\t%d\t%d\t%d\t%d\t%d\n", file.RelPath, file.Bytes, file.MtimeMs, file.Mode, file.ID.Vol, file.ID.Idx)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func previewFromFiles(kind string, files []classifiedFile, percent int, walkTrunc bool) Preview {
	cands := toCandidates(files)
	shown := cands
	trunc := false
	if len(shown) > maxPreviewJSON {
		shown = shown[:maxPreviewJSON]
		trunc = true
	}
	return Preview{
		Percent:    percent,
		Count:      len(cands),
		Bytes:      candidateBytes(files),
		Digest:     planDigest(kind, files),
		Candidates: shown,
		Truncated:  trunc,
		WalkTrunc:  walkTrunc,
	}
}
