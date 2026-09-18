package usageledger

import (
	"testing"
)

func TestSnapshotSeesRotationBetweenListAndOpen(t *testing.T) {
	home := t.TempDir()
	row1 := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	row2 := []byte(`{"timestamp":2,"requestId":"req_2"}`)
	row3 := []byte(`{"timestamp":3,"requestId":"req_3"}`)
	target := int64(len(row1) + 1)
	writer := mustOpen(t, home, target)
	if err := writer.Append(row1); err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(row2); err != nil {
		t.Fatal(err)
	}
	reader := mustOpen(t, home, target)
	testHookAfterListSources = func() {
		testHookAfterListSources = nil
		if err := writer.Append(row3); err != nil {
			t.Errorf("writer rotate: %v", err)
		}
	}
	defer func() { testHookAfterListSources = nil }()
	snap, err := reader.SnapshotNewest(SnapshotLimits{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ids := idsFromLines(t, snap.Lines)
	if len(ids) != 3 {
		t.Fatalf("incoherent snapshot ids=%v", ids)
	}
	if ids[0] != "req_1" || ids[1] != "req_2" || ids[2] != "req_3" {
		t.Fatalf("order=%v", ids)
	}
}

func TestEnumerateSeesRotationBetweenListAndOpen(t *testing.T) {
	home := t.TempDir()
	row1 := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	row2 := []byte(`{"timestamp":2,"requestId":"req_2"}`)
	row3 := []byte(`{"timestamp":3,"requestId":"req_3"}`)
	target := int64(len(row1) + 1)
	writer := mustOpen(t, home, target)
	if err := writer.Append(row1); err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(row2); err != nil {
		t.Fatal(err)
	}
	reader := mustOpen(t, home, target)
	testHookAfterListSources = func() {
		testHookAfterListSources = nil
		if err := writer.Append(row3); err != nil {
			t.Errorf("writer rotate: %v", err)
		}
	}
	defer func() { testHookAfterListSources = nil }()
	var ids []string
	if err := reader.Enumerate(func(line []byte) error {
		ids = append(ids, idsFromLines(t, [][]byte{line})...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[0] != "req_1" || ids[2] != "req_3" {
		t.Fatalf("%v", ids)
	}
}

func TestEnumerateSnapshotExcludesAppendAfterOpen(t *testing.T) {
	home := t.TempDir()
	keep := []byte(`{"timestamp":1,"requestId":"req_keep"}`)
	added := []byte(`{"timestamp":2,"requestId":"req_new"}`)
	writer := mustOpen(t, home, 1<<20)
	if err := writer.Append(keep); err != nil {
		t.Fatal(err)
	}
	reader := mustOpen(t, home, 1<<20)
	testHookAfterOpenSources = func() {
		testHookAfterOpenSources = nil
		if err := writer.Append(added); err != nil {
			t.Errorf("writer append: %v", err)
		}
	}
	defer func() { testHookAfterOpenSources = nil }()
	var ids []string
	logical, err := reader.EnumerateSnapshot(func(line []byte) error {
		ids = append(ids, idsFromLines(t, [][]byte{line})...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := int64(len(keep) + 1)
	if logical != want {
		t.Fatalf("logical=%d want=%d ids=%v", logical, want, ids)
	}
	if len(ids) != 1 || ids[0] != "req_keep" {
		t.Fatalf("rows from a later snapshot: %v", ids)
	}
}
