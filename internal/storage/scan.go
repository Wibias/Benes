package storage

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"time"
)

type BucketKey string

const (
	BucketSessions          BucketKey = "sessions"
	BucketArchivedSessions  BucketKey = "archived_sessions"
	BucketLogsDB            BucketKey = "logs_db"
	BucketStateDB           BucketKey = "state_db"
	BucketAttachments       BucketKey = "attachments"
	BucketDeletionManifests BucketKey = "deletion_manifests"
	BucketOther             BucketKey = "other"
)

var bucketOrder = []BucketKey{
	BucketSessions,
	BucketArchivedSessions,
	BucketLogsDB,
	BucketStateDB,
	BucketAttachments,
	BucketDeletionManifests,
	BucketOther,
}

var bucketLabels = map[BucketKey]string{
	BucketSessions:          "Active sessions",
	BucketArchivedSessions:  "Archived sessions",
	BucketLogsDB:            "Logs database",
	BucketStateDB:           "State database",
	BucketAttachments:       "Attachments",
	BucketDeletionManifests: "Deletion manifests",
	BucketOther:             "Other",
}

var dirBuckets = map[string]BucketKey{
	"sessions":           BucketSessions,
	"archived_sessions":  BucketArchivedSessions,
	"attachments":        BucketAttachments,
	"deletion_manifests": BucketDeletionManifests,
}

var (
	stateDBFile = regexp.MustCompile(`^state_(\d+)\.sqlite(-wal|-shm)?$`)
	logsDBFile  = regexp.MustCompile(`^logs_(\d+)\.sqlite(-wal|-shm)?$`)
)

type LargestEntry struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type Bucket struct {
	Key           BucketKey      `json:"key"`
	Label         string         `json:"label"`
	Bytes         int64          `json:"bytes"`
	FileCount     int            `json:"fileCount"`
	PhysicalBytes int64          `json:"physicalBytes,omitempty"`
	Oldest        *int64         `json:"oldest,omitempty"`
	Newest        *int64         `json:"newest,omitempty"`
	Largest       []LargestEntry `json:"largest,omitempty"`
}

type Report struct {
	CodexHome   string `json:"codexHome"`
	GeneratedAt int64  `json:"generatedAt"`
	Total       struct {
		Bytes         int64 `json:"bytes"`
		FileCount     int   `json:"fileCount"`
		PhysicalBytes int64 `json:"physicalBytes,omitempty"`
	} `json:"total"`
	Buckets   []Bucket `json:"buckets"`
	Truncated bool     `json:"truncated,omitempty"`
}

func Scan(codexHome string) (Report, error) {
	report := Report{CodexHome: codexHome, GeneratedAt: time.Now().UnixMilli()}
	if strings.TrimSpace(codexHome) == "" {
		report.Buckets = emptyBuckets()
		return report, nil
	}
	if _, err := os.Lstat(codexHome); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			report.Buckets = emptyBuckets()
			return report, nil
		}
		return Report{}, err
	}
	root, err := OpenRoot(codexHome)
	if err != nil {
		return Report{}, err
	}
	walked, err := walkHome(root)
	if err != nil {
		return Report{}, err
	}
	byKey := map[BucketKey][]classifiedFile{}
	for _, file := range walked.Files {
		byKey[file.Bucket] = append(byKey[file.Bucket], file)
	}
	report.Buckets = make([]Bucket, 0, len(bucketOrder))
	report.Truncated = walked.Truncated
	globalPhys := map[fileID]int64{}
	unidentified := int64(0)
	for _, file := range walked.Files {
		if file.ID.OK {
			if _, ok := globalPhys[file.ID]; !ok {
				globalPhys[file.ID] = file.Bytes
			}
		} else {
			unidentified += file.Bytes
		}
	}
	for _, key := range bucketOrder {
		bucket := buildBucket(key, byKey[key])
		report.Total.Bytes += bucket.Bytes
		report.Total.FileCount += bucket.FileCount
		report.Buckets = append(report.Buckets, bucket)
	}
	physicalTotal := unidentified
	for _, size := range globalPhys {
		physicalTotal += size
	}
	if physicalTotal != report.Total.Bytes {
		report.Total.PhysicalBytes = physicalTotal
	}
	return report, nil
}

func ArchivedBytes(codexHome string) (int64, int, bool, error) {
	report, err := Scan(codexHome)
	if err != nil {
		return 0, 0, false, err
	}
	for _, bucket := range report.Buckets {
		if bucket.Key == BucketArchivedSessions {
			return bucket.Bytes, bucket.FileCount, report.Truncated, nil
		}
	}
	return 0, 0, report.Truncated, nil
}

func emptyBuckets() []Bucket {
	out := make([]Bucket, 0, len(bucketOrder))
	for _, key := range bucketOrder {
		out = append(out, buildBucket(key, nil))
	}
	return out
}

func buildBucket(key BucketKey, files []classifiedFile) Bucket {
	bucket := Bucket{Key: key, Label: bucketLabels[key], FileCount: len(files)}
	if len(files) == 0 {
		return bucket
	}
	seenPhys := map[fileID]int64{}
	logical := int64(0)
	for _, file := range files {
		logical += file.Bytes
		ms := file.MtimeMs
		if bucket.Oldest == nil || ms < *bucket.Oldest {
			oldest := ms
			bucket.Oldest = &oldest
		}
		if bucket.Newest == nil || ms > *bucket.Newest {
			newest := ms
			bucket.Newest = &newest
		}
		if file.ID.OK {
			if _, ok := seenPhys[file.ID]; !ok {
				seenPhys[file.ID] = file.Bytes
			}
		} else {
			seenPhys[fileID{Vol: uint64(len(seenPhys) + 1), OK: true}] = file.Bytes
		}
	}
	physical := int64(0)
	for _, size := range seenPhys {
		physical += size
	}
	bucket.Bytes = logical
	if physical != logical {
		bucket.PhysicalBytes = physical
	}
	largest := append([]classifiedFile(nil), files...)
	sortLargest(largest)
	if len(largest) > maxLargest {
		largest = largest[:maxLargest]
	}
	bucket.Largest = make([]LargestEntry, 0, len(largest))
	for _, file := range largest {
		bucket.Largest = append(bucket.Largest, LargestEntry{Path: file.RelPath, Bytes: file.Bytes})
	}
	return bucket
}
