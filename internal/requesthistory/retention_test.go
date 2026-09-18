package requesthistory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/usageledger"
)

func TestSchemaV3StoresLedgerOffsetAndEnd(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	meta, err := CatchUp(home)
	if err != nil || meta.SchemaVersion != 3 || meta.RebuildRequired {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	if meta.LedgerEndOffset != meta.SourceSize || meta.IndexedOffset != meta.SourceSize {
		t.Fatalf("offsets %#v", meta)
	}
	db, err := openDB(filepath.Join(home, dbName), dbReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB(db)
	var off int64
	if err := db.QueryRow(`SELECT ledger_offset FROM requests WHERE request_id = ?`, "req_1").Scan(&off); err != nil {
		t.Fatal(err)
	}
	if off != 0 {
		t.Fatalf("offset=%d", off)
	}
}

func TestDuplicateRequestIDKeepsLatestAddress(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	first := []byte(`{"timestamp":1,"requestId":"req_dup","provider":"openai","model":"gpt-5","status":200}`)
	second := []byte(`{"timestamp":2,"requestId":"req_dup","provider":"openai","model":"gpt-5.6","status":201}`)
	if err := l.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(second); err != nil {
		t.Fatal(err)
	}
	meta, err := CatchUp(home)
	if err != nil || meta.IndexedRows != 1 {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	db, err := openDB(filepath.Join(home, dbName), dbReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB(db)
	var off int64
	var raw string
	if err := db.QueryRow(`SELECT ledger_offset, row_json FROM requests WHERE request_id = ?`, "req_dup").Scan(&off, &raw); err != nil {
		t.Fatal(err)
	}
	if off != int64(len(first)+1) {
		t.Fatalf("want second offset %d got %d", len(first)+1, off)
	}
	if !jsonContains(raw, "gpt-5.6") {
		t.Fatalf("row=%s", raw)
	}
}

func TestApplyRetentionCompactsBelowWatermark(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.OpenWithTarget(home, 64)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		row := map[string]any{"timestamp": int64(1000 + i), "requestId": "req_" + strconv.Itoa(i), "provider": "openai", "model": "gpt-5", "status": 200, "pad": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}
		raw, _ := json.Marshal(row)
		if err := l.Append(raw); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	ext, err := l.Extents()
	if err != nil {
		t.Fatal(err)
	}
	policy := usageledger.RetentionPolicy{MaxBytes: ext.RetainedBytes / 2}
	if policy.MaxBytes < 1 {
		policy.MaxBytes = 1
	}
	prev, err := l.PreviewRetention(policy, time.UnixMilli(9000))
	if err != nil || len(prev.DeleteRelPaths) == 0 {
		t.Fatalf("preview=%#v err=%v", prev, err)
	}
	if _, err := l.RunRetention(policy, prev.Digest, time.UnixMilli(9000)); err != nil {
		t.Fatal(err)
	}
	meta, err := ApplyRetention(home)
	if err != nil {
		t.Fatal(err)
	}
	ext2, _ := l.Extents()
	if meta.RetentionWatermark != ext2.RetainedFromOffset {
		t.Fatalf("meta=%#v ext=%#v", meta, ext2)
	}
	db, err := openDB(filepath.Join(home, dbName), dbReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB(db)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM requests WHERE ledger_offset < ?`, ext2.RetainedFromOffset).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("compact left %d rows", n)
	}
}

func TestV2ToV3BackgroundRebuildCandidate(t *testing.T) {
	meta := Meta{SchemaVersion: 2, RebuildRequired: true}
	if !schemaUpgradeRebuildCandidate(meta) {
		t.Fatal("v2 should rebuild")
	}
}

func jsonContains(raw, want string) bool {
	return strings.Contains(raw, want)
}

func TestPerRequestAddressSurvivesRetention(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.OpenWithTarget(home, 64)
	if err != nil {
		t.Fatal(err)
	}
	keep := []byte(`{"timestamp":5000,"requestId":"req_keep","provider":"openai","model":"gpt-5","status":200}`)
	for i := 0; i < 3; i++ {
		row := map[string]any{"timestamp": int64(1000), "requestId": "req_old_" + strconv.Itoa(i), "provider": "openai", "model": "gpt-5", "status": 200, "pad": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}
		raw, _ := json.Marshal(row)
		if err := l.Append(raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Append(keep); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	_, _ = l.Extents()
	policy := usageledger.RetentionPolicy{MaxBytes: int64(len(keep) + 1)}
	prev, err := l.PreviewRetention(policy, time.UnixMilli(9000))
	if err != nil {
		t.Fatal(err)
	}
	if len(prev.DeleteRelPaths) == 0 {
		// force by age for old sealed
		policy = usageledger.RetentionPolicy{MaxAgeMs: 1000}
		prev, err = l.PreviewRetention(policy, time.UnixMilli(9000))
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.RunRetention(policy, prev.Digest, time.UnixMilli(9000)); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRetention(home); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	row, err := Lookup(home, "req_keep")
	if err != nil {
		t.Fatal(err)
	}
	if row["requestId"] != "req_keep" {
		t.Fatalf("%v", row)
	}
	_ = os.Remove
	_ = filepath.Separator
}
