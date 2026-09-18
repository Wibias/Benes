package sessions

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	schemaVersion    = 1
	defaultListLimit = 50
	maxListLimit     = 200
	DefaultRetention = 30 * 24 * time.Hour
	sessionsFileName = "sessions.sqlite"
	pruneReadBound   = 256
	sqlIntegerTime   = `typeof(%s) = 'integer'`
)

var (
	ErrNotFound          = errors.New("session not found")
	ErrInvalidID         = errors.New("invalid session id")
	ErrInvalidCursor     = errors.New("invalid cursor")
	ErrInvalidQuery      = errors.New("invalid session query")
	ErrInvalidFilter     = errors.New("invalid session filter")
	ErrUnavailable       = errors.New("session store unavailable")
	ErrUnsupportedSchema = errors.New("unsupported sessions schema version")
)

type Store struct {
	mu        sync.Mutex
	db        *sql.DB
	path      string
	retention time.Duration
	inflight  map[string]struct{}
	now       func() time.Time
}

func FilePath(home string) string {
	return filepath.Join(strings.TrimSpace(home), sessionsFileName)
}

func Open(path string) (*Store, error) {
	return open(path, nil)
}

func open(path string, now func() time.Time) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("sessions path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA synchronous = NORMAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &Store{db: db, path: path, retention: DefaultRetention, inflight: map[string]struct{}{}, now: now}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.pruneAt(store.clock()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) clock() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *Store) SetRetention(d time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d <= 0 {
		s.retention = DefaultRetention
		return
	}
	s.retention = d
}

func (s *Store) migrate() error {
	version, err := s.readSchemaVersion()
	if err != nil {
		return err
	}
	switch {
	case version == 0:
		if err := s.initSchemaV1(); err != nil {
			return err
		}
		return s.ensureQueryIndexes()
	case version == schemaVersion:
		return s.ensureQueryIndexes()
	default:
		return fmt.Errorf("%w: %d", ErrUnsupportedSchema, version)
	}
}

func (s *Store) readSchemaVersion() (int, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM schema_meta WHERE key = 'schema_version'`).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isNoSuchTable(err) {
			return 0, nil
		}
		return 0, err
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(value))
	if convErr != nil || n < 1 {
		return 0, fmt.Errorf("%w: %q", ErrUnsupportedSchema, value)
	}
	return n, nil
}

func (s *Store) initSchemaV1() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  namespace TEXT NOT NULL,
  external_id TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL,
  last_activity_at INTEGER NOT NULL,
  request_count INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS requests (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  correlation_id TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL,
  protocol TEXT NOT NULL,
  method TEXT NOT NULL,
  path TEXT NOT NULL,
  status INTEGER,
  duration_ms INTEGER,
  request_bytes INTEGER,
  response_bytes INTEGER,
  route_kind TEXT,
  requested_model TEXT,
  requested_provider TEXT,
  resolved_model TEXT,
  provider TEXT,
  combo_id TEXT,
  policy_id TEXT,
  committed_member TEXT,
  attempts_json TEXT,
  input_tokens INTEGER,
  cached_input_tokens INTEGER,
  output_tokens INTEGER,
  total_tokens INTEGER,
  cost REAL,
  currency TEXT,
  FOREIGN KEY (session_id) REFERENCES sessions(id)
);
CREATE TABLE IF NOT EXISTS response_links (
  response_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  FOREIGN KEY (session_id) REFERENCES sessions(id)
);
CREATE UNIQUE INDEX IF NOT EXISTS sessions_identity
  ON sessions(namespace, external_id) WHERE external_id != '';
CREATE INDEX IF NOT EXISTS sessions_last_activity
  ON sessions(last_activity_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS requests_session_started
  ON requests(session_id, started_at ASC, id ASC);
CREATE INDEX IF NOT EXISTS response_links_session
  ON response_links(session_id);
`); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO schema_meta(key, value) VALUES ('schema_version', ?)`, strconv.Itoa(schemaVersion))
	return err
}

func isNoSuchTable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}

func (s *Store) Record(in RecordInput) (string, error) {
	if s == nil {
		return "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return "", ErrUnavailable
	}
	in.RequestID = strings.TrimSpace(in.RequestID)
	if in.RequestID == "" {
		in.RequestID = NewRequestID()
	}
	in.CorrelationID = sanitizeExternalID(in.CorrelationID)
	if in.StartedAt.IsZero() {
		in.StartedAt = s.clock()
	}
	s.inflight[in.RequestID] = struct{}{}
	defer delete(s.inflight, in.RequestID)

	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	sessionID, err := s.resolveSession(tx, in)
	if err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", nil
	}
	sessionID, err = s.insertRequest(tx, sessionID, &in)
	if err != nil {
		return "", err
	}
	if outgoing := sanitizeExternalID(in.OutgoingResponseID); outgoing != "" {
		if err := s.linkOutgoing(tx, sessionID, in.RequestID, outgoing); err != nil {
			return "", err
		}
	}
	if err := s.pruneExpired(tx, s.clock(), in.RequestID, 0); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return sessionID, nil
}

func (s *Store) resolveSession(tx *sql.Tx, in RecordInput) (string, error) {
	identity := in.Identity
	if identity.Groupable() {
		var existing string
		err := tx.QueryRow(`SELECT id FROM sessions WHERE namespace = ? AND external_id = ?`, identity.Namespace, identity.ExternalID).Scan(&existing)
		if err == nil && existing != "" {
			return existing, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	if prev := sanitizeExternalID(in.PreviousResponseID); prev != "" {
		var linked string
		err := tx.QueryRow(`SELECT session_id FROM response_links WHERE response_id = ?`, prev).Scan(&linked)
		if err == nil && linked != "" {
			if identity.Groupable() {
				if err := s.attachIdentity(tx, linked, identity); err != nil {
					return "", err
				}
			}
			return linked, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		if !identity.Groupable() {
			identity = Identity{Namespace: NamespaceResponsesPrevious, ExternalID: prev, Kind: "previous_response"}
		}
	}
	if identity.Groupable() {
		return s.insertSession(tx, identity, in.StartedAt)
	}
	if in.SeedResponsesChain && in.Protocol == ProtocolResponses && sanitizeExternalID(in.OutgoingResponseID) != "" {
		return s.insertSession(tx, Identity{Namespace: NamespaceResponsesChain, Kind: "responses_chain"}, in.StartedAt)
	}
	return "", nil
}

func (s *Store) attachIdentity(tx *sql.Tx, sessionID string, identity Identity) error {
	var currentExt string
	err := tx.QueryRow(`SELECT external_id FROM sessions WHERE id = ?`, sessionID).Scan(&currentExt)
	if err != nil {
		return err
	}
	if currentExt != "" {
		return nil
	}
	var other string
	err = tx.QueryRow(`SELECT id FROM sessions WHERE namespace = ? AND external_id = ?`, identity.Namespace, identity.ExternalID).Scan(&other)
	if err == nil && other != "" && other != sessionID {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(`UPDATE sessions SET namespace = ?, external_id = ? WHERE id = ? AND external_id = ''`, identity.Namespace, identity.ExternalID, sessionID)
	return err
}

func (s *Store) insertSession(tx *sql.Tx, identity Identity, at time.Time) (string, error) {
	id := newSessionID()
	ms := at.UTC().UnixMilli()
	_, err := tx.Exec(
		`INSERT INTO sessions(id, namespace, external_id, started_at, last_activity_at, request_count) VALUES (?, ?, ?, ?, ?, 0)`,
		id, identity.Namespace, identity.ExternalID, ms, ms,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) touchSession(tx *sql.Tx, sessionID string, at time.Time, bump bool) error {
	ms := at.UTC().UnixMilli()
	if bump {
		_, err := tx.Exec(`UPDATE sessions SET last_activity_at = CASE WHEN last_activity_at > ? THEN last_activity_at ELSE ? END, request_count = request_count + 1 WHERE id = ?`, ms, ms, sessionID)
		return err
	}
	_, err := tx.Exec(`UPDATE sessions SET last_activity_at = CASE WHEN last_activity_at > ? THEN last_activity_at ELSE ? END WHERE id = ?`, ms, ms, sessionID)
	return err
}

func (s *Store) linkOutgoing(tx *sql.Tx, sessionID, requestID, outgoing string) error {
	var existingSession string
	err := tx.QueryRow(`SELECT session_id FROM response_links WHERE response_id = ?`, outgoing).Scan(&existingSession)
	if err == nil {
		if existingSession != sessionID {
			return nil
		}
		_, err = tx.Exec(`UPDATE response_links SET request_id = ? WHERE response_id = ? AND session_id = ?`, requestID, outgoing, sessionID)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(`INSERT INTO response_links(response_id, session_id, request_id) VALUES (?, ?, ?)`, outgoing, sessionID, requestID)
	return err
}

func (s *Store) insertRequest(tx *sql.Tx, sessionID string, in *RecordInput) (string, error) {
	if err := s.touchSession(tx, sessionID, in.StartedAt, true); err != nil {
		return "", err
	}
	attempts := ""
	if len(in.Attempts) > 0 {
		raw, err := json.Marshal(in.Attempts)
		if err != nil {
			return "", err
		}
		attempts = string(raw)
	}
	var input, cached, output, total, reqBytes, respBytes any
	var cost any
	currency := ""
	if in.RequestBytes != nil {
		reqBytes = *in.RequestBytes
	}
	if in.ResponseBytes != nil {
		respBytes = *in.ResponseBytes
	}
	if in.Usage != nil {
		input = in.Usage.InputTokens
		cached = in.Usage.CachedInputTokens
		output = in.Usage.OutputTokens
		total = in.Usage.TotalTokens
		if in.Usage.Cost != nil {
			cost = *in.Usage.Cost
			currency = in.Usage.Currency
		}
	}
	for i := 0; i < 8; i++ {
		_, err := tx.Exec(`INSERT INTO requests(
			id, session_id, correlation_id, started_at, protocol, method, path, status, duration_ms,
			request_bytes, response_bytes, route_kind, requested_model, requested_provider,
			resolved_model, provider, combo_id, policy_id, committed_member, attempts_json,
			input_tokens, cached_input_tokens, output_tokens, total_tokens, cost, currency
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			in.RequestID, sessionID, in.CorrelationID, in.StartedAt.UTC().UnixMilli(), in.Protocol, in.Method, in.Path,
			in.Status, in.Duration.Milliseconds(), reqBytes, respBytes,
			nullString(in.Routing.Kind), nullString(in.Routing.RequestedModel), nullString(in.Routing.RequestedProvider),
			nullString(in.Routing.ResolvedModel), nullString(in.Routing.Provider), nullString(in.Routing.ComboID),
			nullString(in.Routing.PolicyID), nullString(in.Routing.CommittedMember), nullString(attempts),
			input, cached, output, total, cost, nullString(currency),
		)
		if err == nil {
			return sessionID, nil
		}
		if !isUniqueConstraint(err) {
			return "", err
		}
		in.RequestID = NewRequestID()
	}
	return "", fmt.Errorf("unable to allocate unique request id")
}

func (s *Store) pruneAt(now time.Time) error {
	return s.pruneAtBound(now, 0)
}

func (s *Store) pruneAtBound(now time.Time, bound int) error {
	if s.db == nil {
		return ErrUnavailable
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.pruneExpired(tx, now, "", bound); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) expireSessionIfStale(id string, now time.Time) error {
	if s.retention <= 0 || strings.TrimSpace(id) == "" {
		return nil
	}
	cutoff := now.UTC().Add(-s.retention).UnixMilli()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var last int64
	err = tx.QueryRow(
		`SELECT last_activity_at FROM sessions WHERE id = ? AND `+fmt.Sprintf(sqlIntegerTime, "last_activity_at"),
		id,
	).Scan(&last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if last >= cutoff {
		return nil
	}
	if err := s.deleteSession(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) pruneExpired(tx *sql.Tx, now time.Time, keepRequestID string, bound int) error {
	if s.retention <= 0 {
		return nil
	}
	cutoff := now.UTC().Add(-s.retention).UnixMilli()
	inflight := make([]any, 0, len(s.inflight))
	for id := range s.inflight {
		inflight = append(inflight, id)
	}
	keepFilter := ""
	args := []any{cutoff}
	if keepRequestID != "" {
		keepFilter = " AND id NOT IN (SELECT session_id FROM requests WHERE id = ?)"
		args = append(args, keepRequestID)
	}
	query := `SELECT id FROM sessions WHERE (NOT (` + fmt.Sprintf(sqlIntegerTime, "last_activity_at") + `) OR last_activity_at < ?)` + keepFilter
	if bound > 0 {
		query += ` LIMIT ?`
		args = append(args, bound)
	}
	rows, err := tx.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil || id == "" {
			continue
		}
		if sessionHasInflight(tx, id, inflight) {
			continue
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.deleteSession(tx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) deleteSession(tx *sql.Tx, id string) error {
	if _, err := tx.Exec(`DELETE FROM response_links WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM requests WHERE session_id = ?`, id); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func sessionHasInflight(tx *sql.Tx, sessionID string, inflight []any) bool {
	if len(inflight) == 0 {
		return false
	}
	for _, requestID := range inflight {
		var found string
		err := tx.QueryRow(`SELECT session_id FROM requests WHERE id = ?`, requestID).Scan(&found)
		if err == nil && found == sessionID {
			return true
		}
	}
	return false
}

func (s *Store) List(opts ListOptions) (ListResult, error) {
	if s == nil {
		return ListResult{Sessions: []Summary{}}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ListResult{}, ErrUnavailable
	}
	if err := s.pruneAtBound(s.clock(), pruneReadBound); err != nil {
		return ListResult{}, err
	}
	if err := validateListOptions(opts); err != nil {
		return ListResult{}, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	cursorTime, cursorID, err := parseCursor(opts.Cursor)
	if err != nil {
		return ListResult{}, err
	}
	cutoff := s.clock().Add(-s.retention).UnixMilli()
	out := make([]Summary, 0, limit+1)
	keyTime, keyID, keyed := cursorTime, cursorID, cursorID != ""
	for len(out) < limit+1 {
		need := limit + 1 - len(out)
		query := `SELECT id, namespace, external_id, started_at, last_activity_at, request_count FROM sessions`
		args := []any{}
		var err error
		query, args, err = s.appendListFilters(query, args, opts, cutoff)
		if err != nil {
			return ListResult{}, err
		}
		if keyed {
			query += ` AND (last_activity_at < ? OR (last_activity_at = ? AND id < ?))`
			args = append(args, keyTime, keyTime, keyID)
		}
		query += ` ORDER BY last_activity_at DESC, id DESC LIMIT ?`
		args = append(args, need)
		rows, err := s.db.Query(query, args...)
		if err != nil {
			return ListResult{}, err
		}
		fetched := 0
		for rows.Next() {
			scanned := scanSessionRow(rows)
			if !scanned.read {
				continue
			}
			fetched++
			if scanned.keyed {
				keyTime, keyID, keyed = scanned.last, scanned.id, true
			}
			if scanned.ok {
				out = append(out, scanned.summary)
			}
		}
		scanErr := rows.Err()
		_ = rows.Close()
		if scanErr != nil {
			return ListResult{}, scanErr
		}
		if fetched < need {
			break
		}
	}
	result := ListResult{Sessions: out}
	if len(out) > limit {
		result.HasMore = true
		result.Sessions = out[:limit]
		last := result.Sessions[len(result.Sessions)-1]
		result.NextCursor = formatCursor(last.LastActivityAt.UnixMilli(), last.ID)
	}
	if result.Sessions == nil {
		result.Sessions = []Summary{}
	}
	if err := s.attachListProtocols(result.Sessions); err != nil {
		return ListResult{}, err
	}
	return result, nil
}

func (s *Store) Get(id string, opts DetailOptions) (Detail, error) {
	id = strings.TrimSpace(id)
	if s == nil {
		return Detail{}, ErrNotFound
	}
	if id == "" || strings.ContainsAny(id, `/\`) || len(id) > 128 {
		return Detail{}, ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return Detail{}, ErrUnavailable
	}
	if err := s.pruneAtBound(s.clock(), pruneReadBound); err != nil {
		return Detail{}, err
	}
	if err := s.expireSessionIfStale(id, s.clock()); err != nil {
		return Detail{}, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	cursorTime, cursorID, err := parseCursor(opts.Cursor)
	if err != nil {
		return Detail{}, err
	}
	summary, err := s.loadSummary(id)
	if err != nil {
		return Detail{}, err
	}
	aggregates, err := s.loadAggregates(id)
	if err != nil {
		return Detail{}, err
	}
	requests := make([]Request, 0, limit+1)
	keyTime, keyID, keyed := cursorTime, cursorID, cursorID != ""
	timeOK := fmt.Sprintf(sqlIntegerTime, "started_at")
	for len(requests) < limit+1 {
		need := limit + 1 - len(requests)
		query := `SELECT id, session_id, correlation_id, started_at, protocol, method, path, status, duration_ms,
			request_bytes, response_bytes, route_kind, requested_model, requested_provider,
			resolved_model, provider, combo_id, policy_id, committed_member, attempts_json,
			input_tokens, cached_input_tokens, output_tokens, total_tokens, cost, currency
			FROM requests WHERE session_id = ? AND ` + timeOK
		args := []any{id}
		if keyed {
			query += ` AND (started_at > ? OR (started_at = ? AND id > ?))`
			args = append(args, keyTime, keyTime, keyID)
		}
		query += ` ORDER BY started_at ASC, id ASC LIMIT ?`
		args = append(args, need)
		rows, err := s.db.Query(query, args...)
		if err != nil {
			return Detail{}, err
		}
		fetched := 0
		for rows.Next() {
			scanned := scanRequestRow(rows)
			if !scanned.read {
				continue
			}
			fetched++
			if scanned.keyed {
				keyTime, keyID, keyed = scanned.started, scanned.id, true
			}
			if scanned.ok {
				requests = append(requests, scanned.request)
			}
		}
		scanErr := rows.Err()
		_ = rows.Close()
		if scanErr != nil {
			return Detail{}, scanErr
		}
		if fetched < need {
			break
		}
	}
	summary.Protocols = append([]string{}, aggregates.Protocols...)
	detail := Detail{Session: summary, Requests: requests, Aggregates: aggregates}
	if len(requests) > limit {
		detail.HasMore = true
		detail.Requests = requests[:limit]
		last := detail.Requests[len(detail.Requests)-1]
		detail.NextCursor = formatCursor(last.StartedAt.UnixMilli(), last.ID)
	}
	if detail.Requests == nil {
		detail.Requests = []Request{}
	}
	return detail, nil
}

func (s *Store) loadSummary(id string) (Summary, error) {
	cutoff := s.clock().Add(-s.retention).UnixMilli()
	row := s.db.QueryRow(
		`SELECT id, namespace, external_id, started_at, last_activity_at, request_count FROM sessions WHERE id = ? AND `+fmt.Sprintf(sqlIntegerTime, "last_activity_at")+` AND last_activity_at >= ?`,
		id, cutoff,
	)
	scanned := scanSessionRow(row)
	if !scanned.ok {
		return Summary{}, ErrNotFound
	}
	return scanned.summary, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

type sessionScan struct {
	read    bool
	keyed   bool
	ok      bool
	id      string
	last    int64
	summary Summary
}

func scanSessionRow(row rowScanner) sessionScan {
	vals, ok := scanValues(row, 6)
	if !ok {
		return sessionScan{}
	}
	id, idOK := valueString(vals[0])
	last, lastOK := valueInt64(vals[4])
	out := sessionScan{read: true, id: id, last: last, keyed: idOK && lastOK}
	namespace, nsOK := valueString(vals[1])
	external, extOK := valueString(vals[2])
	started, startedOK := valueInt64(vals[3])
	count, countOK := valueInt(vals[5])
	if !out.keyed || !nsOK || !extOK || !startedOK || !countOK || !validSessionID(id) {
		return out
	}
	out.ok = true
	out.summary = Summary{
		ID:             id,
		Namespace:      namespace,
		ExternalID:     external,
		StartedAt:      time.UnixMilli(started).UTC(),
		LastActivityAt: time.UnixMilli(last).UTC(),
		RequestCount:   count,
		Protocols:      []string{},
	}
	return out
}

func validSessionID(id string) bool {
	id = strings.TrimSpace(id)
	return strings.HasPrefix(id, "ses_") && len(id) > 4
}

type requestScan struct {
	read    bool
	keyed   bool
	ok      bool
	id      string
	started int64
	request Request
}

func scanRequestRow(row rowScanner) requestScan {
	vals, ok := scanValues(row, 26)
	if !ok {
		return requestScan{}
	}
	id, idOK := valueString(vals[0])
	started, startedOK := valueInt64(vals[3])
	out := requestScan{read: true, id: id, started: started, keyed: idOK && startedOK}
	sessionID, sessionOK := valueString(vals[1])
	correlation, corrOK := valueString(vals[2])
	protocol, protocolOK := valueString(vals[4])
	method, methodOK := valueString(vals[5])
	path, pathOK := valueString(vals[6])
	status, statusOK := valueNullInt64(vals[7])
	duration, durationOK := valueNullInt64(vals[8])
	reqBytes, reqOK := valueNullInt64(vals[9])
	respBytes, respOK := valueNullInt64(vals[10])
	kind, kindOK := valueNullString(vals[11])
	requestedModel, requestedOK := valueNullString(vals[12])
	requestedProv, requestedProvOK := valueNullString(vals[13])
	resolved, resolvedOK := valueNullString(vals[14])
	provider, providerOK := valueNullString(vals[15])
	combo, comboOK := valueNullString(vals[16])
	policy, policyOK := valueNullString(vals[17])
	committed, committedOK := valueNullString(vals[18])
	attemptsJSON, attemptsOK := valueNullString(vals[19])
	input, inputOK := valueNullInt64(vals[20])
	cached, cachedOK := valueNullInt64(vals[21])
	output, outputOK := valueNullInt64(vals[22])
	total, totalOK := valueNullInt64(vals[23])
	cost, costOK := valueNullFloat64(vals[24])
	currency, currencyOK := valueNullString(vals[25])
	if !out.keyed || strings.TrimSpace(id) == "" || !sessionOK || !corrOK || !protocolOK || !methodOK || !pathOK ||
		!statusOK || !durationOK || !reqOK || !respOK || !kindOK || !requestedOK || !requestedProvOK ||
		!resolvedOK || !providerOK || !comboOK || !policyOK || !committedOK || !attemptsOK ||
		!inputOK || !cachedOK || !outputOK || !totalOK || !costOK || !currencyOK {
		return out
	}
	req := Request{
		ID:            id,
		SessionID:     sessionID,
		CorrelationID: correlation,
		StartedAt:     time.UnixMilli(started).UTC(),
		Protocol:      protocol,
		Method:        method,
		Path:          path,
		Status:        int(status.Int64),
		DurationMs:    duration.Int64,
		Routing: Routing{
			Kind:              kind.String,
			RequestedModel:    requestedModel.String,
			RequestedProvider: requestedProv.String,
			ResolvedModel:     resolved.String,
			Provider:          provider.String,
			ComboID:           combo.String,
			PolicyID:          policy.String,
			CommittedMember:   committed.String,
		},
	}
	if reqBytes.Valid {
		value := reqBytes.Int64
		req.RequestBytes = &value
	}
	if respBytes.Valid {
		value := respBytes.Int64
		req.ResponseBytes = &value
	}
	if attemptsJSON.Valid && strings.TrimSpace(attemptsJSON.String) != "" {
		var attempts []Attempt
		if json.Unmarshal([]byte(attemptsJSON.String), &attempts) == nil {
			req.Attempts = attempts
		}
	}
	if input.Valid || cached.Valid || output.Valid || total.Valid || cost.Valid {
		usage := &Usage{Currency: currency.String}
		if input.Valid {
			value := input.Int64
			usage.InputTokens = &value
		}
		if cached.Valid {
			value := cached.Int64
			usage.CachedInputTokens = &value
		}
		if output.Valid {
			value := output.Int64
			usage.OutputTokens = &value
		}
		if total.Valid {
			value := total.Int64
			usage.TotalTokens = &value
		}
		if cost.Valid {
			value := cost.Float64
			usage.Cost = &value
		}
		req.Usage = usage
	}
	out.ok = true
	out.request = req
	return out
}

func scanValues(row rowScanner, n int) ([]any, bool) {
	vals := make([]any, n)
	ptrs := make([]any, n)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if row.Scan(ptrs...) != nil {
		return nil, false
	}
	return vals, true
}

func valueString(v any) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", true
	case string:
		return t, true
	case []byte:
		return string(t), true
	default:
		return "", false
	}
}

func valueNullString(v any) (sql.NullString, bool) {
	text, ok := valueString(v)
	if !ok {
		return sql.NullString{}, false
	}
	if v == nil {
		return sql.NullString{}, true
	}
	return sql.NullString{String: text, Valid: true}, true
}

func valueInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int32:
		return int64(t), true
	case int:
		return int64(t), true
	default:
		return 0, false
	}
}

func valueInt(v any) (int, bool) {
	n, ok := valueInt64(v)
	if !ok || n > int64(^uint(0)>>1) || n < -(int64(^uint(0)>>1))-1 {
		return 0, false
	}
	return int(n), true
}

func valueNullInt64(v any) (sql.NullInt64, bool) {
	if v == nil {
		return sql.NullInt64{}, true
	}
	n, ok := valueInt64(v)
	if !ok {
		return sql.NullInt64{}, false
	}
	return sql.NullInt64{Int64: n, Valid: true}, true
}

func valueNullFloat64(v any) (sql.NullFloat64, bool) {
	if v == nil {
		return sql.NullFloat64{}, true
	}
	switch t := v.(type) {
	case float64:
		return sql.NullFloat64{Float64: t, Valid: true}, true
	case float32:
		return sql.NullFloat64{Float64: float64(t), Valid: true}, true
	case int64:
		return sql.NullFloat64{Float64: float64(t), Valid: true}, true
	case int:
		return sql.NullFloat64{Float64: float64(t), Valid: true}, true
	default:
		return sql.NullFloat64{}, false
	}
}

func parseCursor(raw string) (int64, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, "", nil
	}
	stamp, id, ok := strings.Cut(raw, ":")
	if !ok || strings.TrimSpace(id) == "" {
		return 0, "", ErrInvalidCursor
	}
	ms, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return 0, "", ErrInvalidCursor
	}
	if strings.ContainsAny(id, `/\`) {
		return 0, "", ErrInvalidCursor
	}
	return ms, id, nil
}

func formatCursor(ms int64, id string) string {
	return strconv.FormatInt(ms, 10) + ":" + id
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func NewRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "req_" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return "req_" + hex.EncodeToString(buf[:])
}

func newSessionID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "ses_" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return "ses_" + hex.EncodeToString(buf[:])
}

func isUniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
