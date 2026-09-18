package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanBucketsAndSkipsTrash(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "sessions", "a.jsonl"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "state_5.sqlite"), []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".trash", "epoch"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".trash", "epoch", "secret"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(home)
	if err != nil {
		t.Fatal(err)
	}
	if report.CodexHome != home {
		t.Fatalf("home=%s", report.CodexHome)
	}
	byKey := map[BucketKey]Bucket{}
	for _, bucket := range report.Buckets {
		byKey[bucket.Key] = bucket
	}
	if byKey[BucketSessions].FileCount != 1 || byKey[BucketSessions].Bytes != 5 {
		t.Fatalf("sessions=%+v", byKey[BucketSessions])
	}
	if byKey[BucketStateDB].FileCount != 1 || byKey[BucketStateDB].Bytes != 2 {
		t.Fatalf("state=%+v", byKey[BucketStateDB])
	}
	if byKey[BucketOther].FileCount != 0 {
		t.Fatalf("other=%+v", byKey[BucketOther])
	}
	if report.Total.FileCount != 2 || report.Total.Bytes != 7 {
		t.Fatalf("total=%+v", report.Total)
	}
}

func TestScanMissingHomeIsEmpty(t *testing.T) {
	report, err := Scan(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Total.FileCount != 0 || len(report.Buckets) != 7 {
		t.Fatalf("report=%+v", report)
	}
}
