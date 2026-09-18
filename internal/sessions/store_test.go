package sessions

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestRecordSameIdentitySameSession(t *testing.T) {
	store := openStore(t)
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "thread-a"}
	first, err := store.Record(sampleRecord("req-1", identity, time.UnixMilli(1000), nil))
	if err != nil || first == "" {
		t.Fatalf("first=%q err=%v", first, err)
	}
	second, err := store.Record(sampleRecord("req-2", identity, time.UnixMilli(2000), nil))
	if err != nil || second != first {
		t.Fatalf("second=%q first=%q err=%v", second, first, err)
	}
	detail, err := store.Get(first, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.RequestCount != 2 || len(detail.Requests) != 2 {
		t.Fatalf("count=%d requests=%d", detail.Session.RequestCount, len(detail.Requests))
	}
	if !detail.Session.LastActivityAt.Equal(time.UnixMilli(2000).UTC()) {
		t.Fatalf("last=%s", detail.Session.LastActivityAt)
	}
}

func TestRecordNamespaceCollisionIsolation(t *testing.T) {
	store := openStore(t)
	a, err := store.Record(sampleRecord("req-a", Identity{Namespace: NamespaceCodexThread, ExternalID: "shared"}, time.UnixMilli(1), nil))
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Record(sampleRecord("req-b", Identity{Namespace: "responses/session", ExternalID: "shared"}, time.UnixMilli(2), nil))
	if err != nil || a == "" || b == "" || a == b {
		t.Fatalf("a=%q b=%q err=%v", a, b, err)
	}
}

func TestRecordMissingIdentityIsUngrouped(t *testing.T) {
	store := openStore(t)
	id, err := store.Record(RecordInput{
		RequestID: "req-chat",
		StartedAt: time.UnixMilli(1),
		Protocol:  ProtocolChat,
		Method:    "POST",
		Path:      "/v1/chat/completions",
		Status:    200,
	})
	if err != nil || id != "" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	listed, err := store.List(ListOptions{})
	if err != nil || len(listed.Sessions) != 0 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
}

func TestRecordContinuationLinksPreviousResponse(t *testing.T) {
	store := openStore(t)
	first, err := store.Record(RecordInput{
		RequestID:          "req-1",
		StartedAt:          time.UnixMilli(1),
		Protocol:           ProtocolResponses,
		Method:             "POST",
		Path:               "/v1/responses",
		Status:             200,
		SeedResponsesChain: true,
		OutgoingResponseID: "resp-1",
	})
	if err != nil || first == "" {
		t.Fatalf("first=%q err=%v", first, err)
	}
	second, err := store.Record(RecordInput{
		RequestID:          "req-2",
		StartedAt:          time.UnixMilli(2),
		Protocol:           ProtocolResponses,
		Method:             "POST",
		Path:               "/v1/responses",
		Status:             200,
		PreviousResponseID: "resp-1",
		OutgoingResponseID: "resp-2",
		SeedResponsesChain: true,
	})
	if err != nil || second != first {
		t.Fatalf("second=%q first=%q err=%v", second, first, err)
	}
	detail, err := store.Get(first, DetailOptions{})
	if err != nil || len(detail.Requests) != 2 {
		t.Fatalf("detail=%#v err=%v", detail, err)
	}
}

func TestRecordUnknownPreviousResponseSeedsDeterministically(t *testing.T) {
	store := openStore(t)
	first, err := store.Record(RecordInput{
		RequestID:          "req-2",
		StartedAt:          time.UnixMilli(2),
		Protocol:           ProtocolResponses,
		Method:             "POST",
		Path:               "/v1/responses",
		Status:             200,
		PreviousResponseID: "resp-missing",
		OutgoingResponseID: "resp-2",
	})
	if err != nil || first == "" {
		t.Fatalf("first=%q err=%v", first, err)
	}
	seed, err := store.Get(first, DetailOptions{})
	if err != nil || seed.Session.Namespace != NamespaceResponsesPrevious || seed.Session.ExternalID != "resp-missing" {
		t.Fatalf("seed=%#v err=%v", seed.Session, err)
	}
	second, err := store.Record(RecordInput{
		RequestID:          "req-3",
		StartedAt:          time.UnixMilli(3),
		Protocol:           ProtocolResponses,
		Method:             "POST",
		Path:               "/v1/responses",
		Status:             200,
		PreviousResponseID: "resp-2",
	})
	if err != nil || second != first {
		t.Fatalf("second=%q first=%q err=%v", second, first, err)
	}
}

func TestPersistenceSurvivesStoreRecreation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.sqlite")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Minute)
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "durable"}
	sessionID, err := store.Record(sampleRecord("req-1", identity, at, int64Ptr(11)))
	if err != nil || sessionID == "" {
		t.Fatalf("id=%q err=%v", sessionID, err)
	}
	if _, err := store.Record(sampleRecord("req-2", identity, at.Add(time.Second), int64Ptr(22))); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	listed, err := reopened.List(ListOptions{})
	if err != nil || len(listed.Sessions) != 1 || listed.Sessions[0].ID != sessionID || listed.Sessions[0].RequestCount != 2 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	detail, err := reopened.Get(sessionID, DetailOptions{})
	if err != nil || len(detail.Requests) != 2 || detail.Requests[0].ID != "req-1" || detail.Requests[1].ID != "req-2" {
		t.Fatalf("detail=%#v err=%v", detail, err)
	}
	if detail.Requests[0].Usage == nil || detail.Requests[0].Usage.InputTokens == nil || *detail.Requests[0].Usage.InputTokens != 11 {
		t.Fatalf("usage=%#v", detail.Requests[0].Usage)
	}
}

func TestConcurrentWritesSameSession(t *testing.T) {
	store := openStore(t)
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "concurrent"}
	var wg sync.WaitGroup
	ids := make([]string, 20)
	errs := make([]error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = store.Record(sampleRecord("req-"+strconv.Itoa(i), identity, time.UnixMilli(int64(i+1)), nil))
		}(i)
	}
	wg.Wait()
	sessionID := ""
	for i, err := range errs {
		if err != nil || ids[i] == "" {
			t.Fatalf("i=%d id=%q err=%v", i, ids[i], err)
		}
		if sessionID == "" {
			sessionID = ids[i]
		}
		if ids[i] != sessionID {
			t.Fatalf("session split %q vs %q", sessionID, ids[i])
		}
	}
	detail, err := store.Get(sessionID, DetailOptions{Limit: 200})
	if err != nil || detail.Session.RequestCount != 20 || len(detail.Requests) != 20 {
		t.Fatalf("count=%d requests=%d err=%v", detail.Session.RequestCount, len(detail.Requests), err)
	}
	for i := 1; i < len(detail.Requests); i++ {
		if !detail.Requests[i].StartedAt.After(detail.Requests[i-1].StartedAt) && detail.Requests[i].ID <= detail.Requests[i-1].ID {
			t.Fatalf("unordered %#v", detail.Requests)
		}
	}
}

func TestListAndDetailPagination(t *testing.T) {
	store := openStore(t)
	var first string
	for i := 0; i < 3; i++ {
		id, err := store.Record(sampleRecord("s"+strconv.Itoa(i)+"-a", Identity{Namespace: NamespaceCodexThread, ExternalID: "t" + strconv.Itoa(i)}, time.UnixMilli(int64((i+1)*1000)), nil))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = id
		}
		if _, err := store.Record(sampleRecord("s"+strconv.Itoa(i)+"-b", Identity{Namespace: NamespaceCodexThread, ExternalID: "t" + strconv.Itoa(i)}, time.UnixMilli(int64((i+1)*1000+1)), nil)); err != nil {
			t.Fatal(err)
		}
	}
	page1, err := store.List(ListOptions{Limit: 2})
	if err != nil || !page1.HasMore || len(page1.Sessions) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1=%#v err=%v", page1, err)
	}
	page2, err := store.List(ListOptions{Limit: 2, Cursor: page1.NextCursor})
	if err != nil || page2.HasMore || len(page2.Sessions) != 1 {
		t.Fatalf("page2=%#v err=%v", page2, err)
	}
	detail1, err := store.Get(first, DetailOptions{Limit: 1})
	if err != nil || !detail1.HasMore || len(detail1.Requests) != 1 || detail1.Requests[0].ID != "s0-a" {
		t.Fatalf("detail1=%#v err=%v", detail1, err)
	}
	detail2, err := store.Get(first, DetailOptions{Limit: 1, Cursor: detail1.NextCursor})
	if err != nil || detail2.HasMore || len(detail2.Requests) != 1 || detail2.Requests[0].ID != "s0-b" {
		t.Fatalf("detail2=%#v err=%v", detail2, err)
	}
}

func TestGetUnknownAndInvalidID(t *testing.T) {
	store := openStore(t)
	if _, err := store.Get("ses_missing", DetailOptions{}); err != ErrNotFound {
		t.Fatalf("err=%v", err)
	}
	if _, err := store.Get("../etc/passwd", DetailOptions{}); err != ErrInvalidID {
		t.Fatalf("err=%v", err)
	}
	if _, err := store.List(ListOptions{Cursor: "bad"}); err != ErrInvalidCursor {
		t.Fatalf("err=%v", err)
	}
}

func TestUsageAbsentWhenNotProvided(t *testing.T) {
	store := openStore(t)
	id, err := store.Record(RecordInput{
		RequestID: "req-1",
		StartedAt: time.UnixMilli(1),
		Protocol:  ProtocolResponses,
		Method:    "POST",
		Path:      "/v1/responses",
		Status:    200,
		Identity:  Identity{Namespace: NamespaceCodexThread, ExternalID: "t"},
		Routing:   Routing{Kind: "direct", RequestedModel: "openai-apikey/gpt-5", ResolvedModel: "gpt-5", Provider: "openai-apikey"},
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := store.Get(id, DetailOptions{})
	if err != nil || detail.Requests[0].Usage != nil {
		t.Fatalf("usage invented %#v", detail.Requests[0].Usage)
	}
	if detail.Requests[0].Routing.Provider != "openai-apikey" || detail.Requests[0].Routing.RequestedModel != "openai-apikey/gpt-5" {
		t.Fatalf("routing %#v", detail.Requests[0].Routing)
	}
}

func TestRetentionDeletesInactiveSessions(t *testing.T) {
	store := openStore(t)
	store.SetRetention(time.Hour)
	oldID, err := store.Record(sampleRecord("old", Identity{Namespace: NamespaceCodexThread, ExternalID: "old"}, time.UnixMilli(1), nil))
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1).Add(2 * time.Hour)
	newID, err := store.Record(sampleRecord("new", Identity{Namespace: NamespaceCodexThread, ExternalID: "new"}, now, nil))
	if err != nil || newID == "" {
		t.Fatalf("new=%q err=%v", newID, err)
	}
	if _, err := store.Get(oldID, DetailOptions{}); err != ErrNotFound {
		t.Fatalf("old survived err=%v", err)
	}
	if _, err := store.Get(newID, DetailOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedRowDoesNotBreakList(t *testing.T) {
	store := openStore(t)
	if _, err := store.Record(sampleRecord("req-1", Identity{Namespace: NamespaceCodexThread, ExternalID: "ok"}, time.UnixMilli(5), nil)); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	_, err := store.db.Exec(`INSERT INTO sessions(id, namespace, external_id, started_at, last_activity_at, request_count) VALUES ('', 'broken', 'x', 1, 1, 1)`)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(ListOptions{})
	if err != nil || len(listed.Sessions) != 1 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
}

func TestRecordChainRequiresOutgoingID(t *testing.T) {
	store := openStore(t)
	id, err := store.Record(RecordInput{
		RequestID:          "req-no-out",
		StartedAt:          time.UnixMilli(1),
		Protocol:           ProtocolResponses,
		Method:             "POST",
		Path:               "/v1/responses",
		Status:             400,
		SeedResponsesChain: true,
	})
	if err != nil || id != "" {
		t.Fatalf("fabricated session id=%q err=%v", id, err)
	}
}

func TestRecordDoesNotRebindResponseLinkAcrossSessions(t *testing.T) {
	store := openStore(t)
	first, err := store.Record(RecordInput{
		RequestID:          "req-a",
		StartedAt:          time.UnixMilli(1),
		Protocol:           ProtocolResponses,
		Method:             "POST",
		Path:               "/v1/responses",
		Status:             200,
		Identity:           Identity{Namespace: NamespaceCodexThread, ExternalID: "thread-a"},
		OutgoingResponseID: "resp-shared",
	})
	if err != nil || first == "" {
		t.Fatalf("first=%q err=%v", first, err)
	}
	second, err := store.Record(RecordInput{
		RequestID:          "req-b",
		StartedAt:          time.UnixMilli(2),
		Protocol:           ProtocolResponses,
		Method:             "POST",
		Path:               "/v1/responses",
		Status:             200,
		Identity:           Identity{Namespace: NamespaceCodexThread, ExternalID: "thread-b"},
		OutgoingResponseID: "resp-shared",
	})
	if err != nil || second == "" || second == first {
		t.Fatalf("second=%q first=%q err=%v", second, first, err)
	}
	joined, err := store.Record(RecordInput{
		RequestID:          "req-c",
		StartedAt:          time.UnixMilli(3),
		Protocol:           ProtocolResponses,
		Method:             "POST",
		Path:               "/v1/responses",
		Status:             200,
		PreviousResponseID: "resp-shared",
	})
	if err != nil || joined != first {
		t.Fatalf("joined=%q want %q err=%v", joined, first, err)
	}
}

func TestListPaginationSkipsMalformedRowWithoutStranding(t *testing.T) {
	store := openStore(t)
	insertSession(t, store, "ses_aaa0000000000000000000000000000", "a", 3000)
	insertSession(t, store, "zzz-malformed", "broken", 2500)
	insertSession(t, store, "ses_bbb0000000000000000000000000000", "b", 2000)
	insertSession(t, store, "ses_ccc0000000000000000000000000000", "c", 1000)
	page1, err := store.List(ListOptions{Limit: 1})
	if err != nil || !page1.HasMore || len(page1.Sessions) != 1 || page1.Sessions[0].ID != "ses_aaa0000000000000000000000000000" {
		t.Fatalf("page1=%#v err=%v", page1, err)
	}
	page2, err := store.List(ListOptions{Limit: 1, Cursor: page1.NextCursor})
	if err != nil || !page2.HasMore || len(page2.Sessions) != 1 || page2.Sessions[0].ID != "ses_bbb0000000000000000000000000000" {
		t.Fatalf("page2=%#v err=%v", page2, err)
	}
	page3, err := store.List(ListOptions{Limit: 1, Cursor: page2.NextCursor})
	if err != nil || page3.HasMore || len(page3.Sessions) != 1 || page3.Sessions[0].ID != "ses_ccc0000000000000000000000000000" {
		t.Fatalf("page3=%#v err=%v", page3, err)
	}
}

func TestGetPaginationSkipsMalformedRequestWithoutStranding(t *testing.T) {
	store := openStore(t)
	id, err := store.Record(sampleRecord("req-a", Identity{Namespace: NamespaceCodexThread, ExternalID: "t"}, time.UnixMilli(1000), nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(sampleRecord("req-c", Identity{Namespace: NamespaceCodexThread, ExternalID: "t"}, time.UnixMilli(3000), nil)); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	_, err = store.db.Exec(`INSERT INTO requests(id, session_id, started_at, protocol, method, path) VALUES ('', ?, 2000, 'responses', 'POST', '/v1/responses')`, id)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	page1, err := store.Get(id, DetailOptions{Limit: 1})
	if err != nil || !page1.HasMore || len(page1.Requests) != 1 || page1.Requests[0].ID != "req-a" {
		t.Fatalf("page1=%#v err=%v", page1, err)
	}
	page2, err := store.Get(id, DetailOptions{Limit: 1, Cursor: page1.NextCursor})
	if err != nil || page2.HasMore || len(page2.Requests) != 1 || page2.Requests[0].ID != "req-c" {
		t.Fatalf("page2=%#v err=%v", page2, err)
	}
}

func TestRetentionPrunesOnOpenWithoutNewRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.sqlite")
	now := time.UnixMilli(1_700_000_000_000)
	store, err := open(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	oldID, err := store.Record(sampleRecord("old", Identity{Namespace: NamespaceCodexThread, ExternalID: "old"}, now.Add(-31*24*time.Hour), nil))
	if err != nil || oldID == "" {
		t.Fatalf("id=%q err=%v", oldID, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := open(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	listed, err := reopened.List(ListOptions{})
	if err != nil || len(listed.Sessions) != 0 {
		t.Fatalf("expired session survived listed=%#v err=%v", listed, err)
	}
	if _, err := reopened.Get(oldID, DetailOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired detail err=%v", err)
	}
}

func TestOpenRejectsFutureSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO schema_meta(key, value) VALUES ('schema_version', '2');
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  namespace TEXT NOT NULL,
  external_id TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL,
  last_activity_at INTEGER NOT NULL,
  request_count INTEGER NOT NULL DEFAULT 0
);
INSERT INTO sessions(id, namespace, external_id, started_at, last_activity_at, request_count)
VALUES ('ses_future', 'future', 'keep', 1, 1, 1);
`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("err=%v", err)
	}
	verify, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = verify.Close() })
	var version, id string
	if err := verify.QueryRow(`SELECT value FROM schema_meta WHERE key = 'schema_version'`).Scan(&version); err != nil || version != "2" {
		t.Fatalf("version=%q err=%v", version, err)
	}
	if err := verify.QueryRow(`SELECT id FROM sessions WHERE id = 'ses_future'`).Scan(&id); err != nil || id != "ses_future" {
		t.Fatalf("row mutated id=%q err=%v", id, err)
	}
}

func TestClosedStoreIsUnavailable(t *testing.T) {
	store := openStore(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ListOptions{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("list err=%v", err)
	}
	if _, err := store.Get("ses_missing", DetailOptions{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("get err=%v", err)
	}
	if _, err := store.Record(sampleRecord("req", Identity{Namespace: NamespaceCodexThread, ExternalID: "t"}, time.UnixMilli(1), nil)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("record err=%v", err)
	}
}

func TestRetentionPrunesOnListWithoutRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.sqlite")
	clk := &testClock{t: time.UnixMilli(1_700_000_000_000)}
	store, err := open(path, clk.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	id, err := store.Record(sampleRecord("old", Identity{Namespace: NamespaceCodexThread, ExternalID: "old"}, clk.now(), nil))
	if err != nil || id == "" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if _, err := store.Get(id, DetailOptions{}); err != nil {
		t.Fatal(err)
	}
	clk.set(clk.now().Add(31 * 24 * time.Hour))
	listed, err := store.List(ListOptions{})
	if err != nil || len(listed.Sessions) != 0 {
		t.Fatalf("list after expiry %#v err=%v", listed, err)
	}
	if _, err := store.Get(id, DetailOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("detail after expiry err=%v", err)
	}
	store.mu.Lock()
	var leftover int
	if err := store.db.QueryRow(`SELECT COUNT(1) FROM requests`).Scan(&leftover); err != nil || leftover != 0 {
		store.mu.Unlock()
		t.Fatalf("requests leftover=%d err=%v", leftover, err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(1) FROM response_links`).Scan(&leftover); err != nil || leftover != 0 {
		store.mu.Unlock()
		t.Fatalf("links leftover=%d err=%v", leftover, err)
	}
	store.mu.Unlock()
}

func TestRecordMintsUniqueRequestIDs(t *testing.T) {
	store := openStore(t)
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "mint"}
	first, err := store.Record(sampleRecord("", identity, time.UnixMilli(1), nil))
	if err != nil || first == "" {
		t.Fatalf("first=%q err=%v", first, err)
	}
	second, err := store.Record(sampleRecord("", identity, time.UnixMilli(2), nil))
	if err != nil || second != first {
		t.Fatalf("second=%q first=%q err=%v", second, first, err)
	}
	detail, err := store.Get(first, DetailOptions{})
	if err != nil || detail.Session.RequestCount != 2 || len(detail.Requests) != 2 {
		t.Fatalf("detail=%#v err=%v", detail, err)
	}
	if detail.Requests[0].ID == "" || detail.Requests[0].ID == detail.Requests[1].ID {
		t.Fatalf("ids %#v %#v", detail.Requests[0].ID, detail.Requests[1].ID)
	}
	if !strings.HasPrefix(detail.Requests[0].ID, "req_") || !strings.HasPrefix(detail.Requests[1].ID, "req_") {
		t.Fatalf("expected minted req_ ids %#v", detail.Requests)
	}
}

func TestRecordSameCorrelationDoesNotCollideSessions(t *testing.T) {
	store := openStore(t)
	a, err := store.Record(RecordInput{
		RequestID:          "req-internal-a",
		CorrelationID:      "shared-client",
		StartedAt:          time.UnixMilli(1),
		Protocol:           ProtocolResponses,
		Method:             "POST",
		Path:               "/v1/responses",
		Status:             200,
		Identity:           Identity{Namespace: NamespaceCodexThread, ExternalID: "thread-a"},
		OutgoingResponseID: "resp-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Record(RecordInput{
		RequestID:          "req-internal-b",
		CorrelationID:      "shared-client",
		StartedAt:          time.UnixMilli(2),
		Protocol:           ProtocolResponses,
		Method:             "POST",
		Path:               "/v1/responses",
		Status:             200,
		Identity:           Identity{Namespace: NamespaceCodexThread, ExternalID: "thread-b"},
		OutgoingResponseID: "resp-b",
	})
	if err != nil || a == "" || b == "" || a == b {
		t.Fatalf("a=%q b=%q err=%v", a, b, err)
	}
	listed, err := store.List(ListOptions{})
	if err != nil || len(listed.Sessions) != 2 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	for _, sessionID := range []string{a, b} {
		detail, err := store.Get(sessionID, DetailOptions{})
		if err != nil || detail.Session.RequestCount != 1 || len(detail.Requests) != 1 {
			t.Fatalf("detail %s %#v err=%v", sessionID, detail, err)
		}
		if detail.Requests[0].CorrelationID != "shared-client" {
			t.Fatalf("correlation %#v", detail.Requests[0])
		}
	}
	joined, err := store.Record(RecordInput{
		RequestID:          "req-internal-c",
		StartedAt:          time.UnixMilli(3),
		Protocol:           ProtocolResponses,
		Method:             "POST",
		Path:               "/v1/responses",
		Status:             200,
		PreviousResponseID: "resp-b",
	})
	if err != nil || joined != b {
		t.Fatalf("joined=%q want %q err=%v", joined, b, err)
	}
}

func TestListPaginationSkipsCorruptTimestampWithoutStranding(t *testing.T) {
	store := openStore(t)
	insertSession(t, store, "ses_aaa0000000000000000000000000000", "a", 3000)
	store.mu.Lock()
	_, err := store.db.Exec(`INSERT INTO sessions(id, namespace, external_id, started_at, last_activity_at, request_count) VALUES ('ses_bad0000000000000000000000000000', 'codex/thread', 'bad', 2500, 'not-an-int', 1)`)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	insertSession(t, store, "ses_ccc0000000000000000000000000000", "c", 1000)
	page1, err := store.List(ListOptions{Limit: 1})
	if err != nil || !page1.HasMore || len(page1.Sessions) != 1 || page1.Sessions[0].ID != "ses_aaa0000000000000000000000000000" {
		t.Fatalf("page1=%#v err=%v", page1, err)
	}
	page2, err := store.List(ListOptions{Limit: 1, Cursor: page1.NextCursor})
	if err != nil || page2.HasMore || len(page2.Sessions) != 1 || page2.Sessions[0].ID != "ses_ccc0000000000000000000000000000" {
		t.Fatalf("page2=%#v err=%v", page2, err)
	}
}

func TestGetPaginationSkipsCorruptTimestampWithoutStranding(t *testing.T) {
	store := openStore(t)
	id, err := store.Record(sampleRecord("req-a", Identity{Namespace: NamespaceCodexThread, ExternalID: "t"}, time.UnixMilli(1000), nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(sampleRecord("req-c", Identity{Namespace: NamespaceCodexThread, ExternalID: "t"}, time.UnixMilli(3000), nil)); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	_, err = store.db.Exec(`INSERT INTO requests(id, session_id, started_at, protocol, method, path) VALUES ('req-bad', ?, 'not-an-int', 'responses', 'POST', '/v1/responses')`, id)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	page1, err := store.Get(id, DetailOptions{Limit: 1})
	if err != nil || !page1.HasMore || len(page1.Requests) != 1 || page1.Requests[0].ID != "req-a" {
		t.Fatalf("page1=%#v err=%v", page1, err)
	}
	page2, err := store.Get(id, DetailOptions{Limit: 1, Cursor: page1.NextCursor})
	if err != nil || page2.HasMore || len(page2.Requests) != 1 || page2.Requests[0].ID != "req-c" {
		t.Fatalf("page2=%#v err=%v", page2, err)
	}
}

func TestListPaginationSkipsCorruptNonOrderingFieldsWithoutStranding(t *testing.T) {
	store := openStore(t)
	insertSession(t, store, "ses_aaa0000000000000000000000000000", "a", 5000)
	store.mu.Lock()
	_, err := store.db.Exec(`INSERT INTO sessions(id, namespace, external_id, started_at, last_activity_at, request_count) VALUES ('ses_badstarted00000000000000000000', 'codex/thread', 'bad-started', 'not-an-int', 4000, 1)`)
	if err == nil {
		_, err = store.db.Exec(`INSERT INTO sessions(id, namespace, external_id, started_at, last_activity_at, request_count) VALUES ('ses_badcount000000000000000000000', 'codex/thread', 'bad-count', 2000, 2000, 'not-a-count')`)
	}
	if err == nil {
		_, err = store.db.Exec(`INSERT INTO sessions(id, namespace, external_id, started_at, last_activity_at, request_count) VALUES ('ses_real0000000000000000000000000', 'codex/thread', 'real', 2500, 2500.5, 1)`)
	}
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	insertSession(t, store, "ses_bbb0000000000000000000000000000", "b", 3000)
	insertSession(t, store, "ses_ccc0000000000000000000000000000", "c", 1000)
	page1, err := store.List(ListOptions{Limit: 1})
	if err != nil || !page1.HasMore || len(page1.Sessions) != 1 || page1.Sessions[0].ID != "ses_aaa0000000000000000000000000000" {
		t.Fatalf("page1=%#v err=%v", page1, err)
	}
	if page1.Sessions[0].StartedAt.IsZero() || page1.Sessions[0].RequestCount == 0 {
		t.Fatalf("coerced zero summary %#v", page1.Sessions[0])
	}
	page2, err := store.List(ListOptions{Limit: 1, Cursor: page1.NextCursor})
	if err != nil || !page2.HasMore || len(page2.Sessions) != 1 || page2.Sessions[0].ID != "ses_bbb0000000000000000000000000000" {
		t.Fatalf("page2=%#v err=%v", page2, err)
	}
	page3, err := store.List(ListOptions{Limit: 1, Cursor: page2.NextCursor})
	if err != nil || page3.HasMore || len(page3.Sessions) != 1 || page3.Sessions[0].ID != "ses_ccc0000000000000000000000000000" {
		t.Fatalf("page3=%#v err=%v", page3, err)
	}
	listed, err := store.List(ListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range listed.Sessions {
		switch row.ID {
		case "ses_badstarted00000000000000000000", "ses_badcount000000000000000000000", "ses_real0000000000000000000000000":
			t.Fatalf("malformed row surfaced %#v", row)
		}
		if row.StartedAt.IsZero() && row.LastActivityAt.IsZero() {
			t.Fatalf("zero-valued summary %#v", row)
		}
	}
	if len(listed.Sessions) != 3 {
		t.Fatalf("listed=%#v", listed)
	}
}

func TestGetPaginationSkipsCorruptNonOrderingFieldsWithoutStranding(t *testing.T) {
	store := openStore(t)
	id, err := store.Record(sampleRecord("req-a", Identity{Namespace: NamespaceCodexThread, ExternalID: "t"}, time.UnixMilli(1000), nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(sampleRecord("req-b", Identity{Namespace: NamespaceCodexThread, ExternalID: "t"}, time.UnixMilli(3000), nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(sampleRecord("req-c", Identity{Namespace: NamespaceCodexThread, ExternalID: "t"}, time.UnixMilli(5000), nil)); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	_, err = store.db.Exec(`INSERT INTO requests(id, session_id, started_at, protocol, method, path, status) VALUES ('req-bad-status', ?, 2000, 'responses', 'POST', '/v1/responses', 'not-a-status')`, id)
	if err == nil {
		_, err = store.db.Exec(`INSERT INTO requests(id, session_id, started_at, protocol, method, path, cost) VALUES ('req-bad-cost', ?, 4000, 'responses', 'POST', '/v1/responses', 'not-a-cost')`, id)
	}
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	page1, err := store.Get(id, DetailOptions{Limit: 1})
	if err != nil || !page1.HasMore || len(page1.Requests) != 1 || page1.Requests[0].ID != "req-a" {
		t.Fatalf("page1=%#v err=%v", page1, err)
	}
	if page1.Requests[0].Status == 0 && page1.Requests[0].StartedAt.IsZero() {
		t.Fatalf("coerced zero request %#v", page1.Requests[0])
	}
	page2, err := store.Get(id, DetailOptions{Limit: 1, Cursor: page1.NextCursor})
	if err != nil || !page2.HasMore || len(page2.Requests) != 1 || page2.Requests[0].ID != "req-b" {
		t.Fatalf("page2=%#v err=%v", page2, err)
	}
	page3, err := store.Get(id, DetailOptions{Limit: 1, Cursor: page2.NextCursor})
	if err != nil || page3.HasMore || len(page3.Requests) != 1 || page3.Requests[0].ID != "req-c" {
		t.Fatalf("page3=%#v err=%v", page3, err)
	}
	detail, err := store.Get(id, DetailOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range detail.Requests {
		if row.ID == "req-bad-status" || row.ID == "req-bad-cost" {
			t.Fatalf("malformed request surfaced %#v", row)
		}
	}
	if len(detail.Requests) != 3 {
		t.Fatalf("detail=%#v", detail)
	}
}

func TestCloseConcurrentWithReadsAndWrites(t *testing.T) {
	store := openStore(t)
	identity := Identity{Namespace: NamespaceCodexThread, ExternalID: "race"}
	var wg sync.WaitGroup
	var started sync.WaitGroup
	stop := make(chan struct{})
	started.Add(4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			started.Done()
			n := 0
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = store.Record(sampleRecord("race-"+strconv.Itoa(i)+"-"+strconv.Itoa(n), identity, time.UnixMilli(int64(n+1)), nil))
					_, _ = store.List(ListOptions{Limit: 10})
					n++
				}
			}
		}(i)
	}
	started.Wait()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	close(stop)
	wg.Wait()
	if _, err := store.List(ListOptions{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("list after close err=%v", err)
	}
}

func insertSession(t *testing.T, store *Store, id, external string, lastActivity int64) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	_, err := store.db.Exec(`INSERT INTO sessions(id, namespace, external_id, started_at, last_activity_at, request_count) VALUES (?, 'codex/thread', ?, ?, ?, 1)`, id, external, lastActivity, lastActivity)
	if err != nil {
		t.Fatal(err)
	}
}

func openStore(t *testing.T) *Store {
	t.Helper()
	frozen := time.UnixMilli(10_000_000)
	store, err := open(filepath.Join(t.TempDir(), "sessions.sqlite"), func() time.Time { return frozen })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func sampleRecord(id string, identity Identity, at time.Time, inputTokens *int64) RecordInput {
	in := RecordInput{
		RequestID: id,
		StartedAt: at,
		Protocol:  ProtocolResponses,
		Method:    "POST",
		Path:      "/v1/responses",
		Status:    200,
		Duration:  12 * time.Millisecond,
		Identity:  identity,
	}
	if inputTokens != nil {
		output := int64(3)
		in.Usage = &Usage{InputTokens: inputTokens, OutputTokens: &output}
	}
	return in
}

func int64Ptr(v int64) *int64 { return &v }

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t.UTC()
}

func (c *testClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t.UTC()
}
