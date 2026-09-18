package requesthistory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wibias/Benes/internal/usageledger"

	_ "modernc.org/sqlite"
)

const (
	schemaVersion = 3
	dbName        = "routing-history.sqlite"
)

var (
	ErrRebuildRequired = errors.New("request-history index requires rebuild")
	ErrUnavailable     = errors.New("request-history index unavailable")

	testHookMu                         sync.Mutex
	testHookBeforeCatchUp              func() error
	testHookHoldCatchUp                func()
	testHookAfterRows                  func() error
	testHookBeforeCommit               func() error
	testHookCountTable                 func()
	testHookInstall                    func(tmp, dest string) error
	testHookReplace                    func(tmp, dest string) error
	testHookAcquireWriterWaiting       func()
	testHookBeforeAcquireWriterAttempt func()
	testHookAfterIndexerStop           func()

	rebuildSeq atomic.Uint64
)

func SetBeforeCatchUpTestHook(fn func() error) {
	testHookMu.Lock()
	testHookBeforeCatchUp = fn
	testHookMu.Unlock()
}

func SetHoldCatchUpTestHook(fn func()) {
	testHookMu.Lock()
	testHookHoldCatchUp = fn
	testHookMu.Unlock()
}

func SetInstallTestHook(fn func(tmp, dest string) error) {
	testHookMu.Lock()
	testHookInstall = fn
	testHookMu.Unlock()
}

func SetAfterRowsTestHook(fn func() error) {
	testHookMu.Lock()
	testHookAfterRows = fn
	testHookMu.Unlock()
}

func SetBeforeCommitTestHook(fn func() error) {
	testHookMu.Lock()
	testHookBeforeCommit = fn
	testHookMu.Unlock()
}

func SetCountTableTestHook(fn func()) {
	testHookMu.Lock()
	testHookCountTable = fn
	testHookMu.Unlock()
}

func SetReplaceTestHook(fn func(tmp, dest string) error) {
	testHookMu.Lock()
	testHookReplace = fn
	testHookMu.Unlock()
}

func SetAcquireWriterWaitingTestHook(fn func()) {
	testHookMu.Lock()
	testHookAcquireWriterWaiting = fn
	testHookMu.Unlock()
}

func SetBeforeAcquireWriterAttemptTestHook(fn func()) {
	testHookMu.Lock()
	testHookBeforeAcquireWriterAttempt = fn
	testHookMu.Unlock()
}

func SetAfterIndexerStopTestHook(fn func()) {
	testHookMu.Lock()
	testHookAfterIndexerStop = fn
	testHookMu.Unlock()
}

type Meta struct {
	SchemaVersion      int    `json:"schemaVersion"`
	DBPath             string `json:"dbPath"`
	SourceSize         int64  `json:"sourceSize"`
	LedgerEndOffset    int64  `json:"ledgerEndOffset"`
	RetentionWatermark int64  `json:"retentionWatermark"`
	IndexedOffset      int64  `json:"indexedOffset"`
	IndexedRows        int    `json:"indexedRows"`
	PendingBytes       int64  `json:"pendingBytes"`
	CaughtUp           bool   `json:"caughtUp"`
	RebuildRequired    bool   `json:"rebuildRequired"`
	LastError          string `json:"lastError"`
	UpdatedAtMs        int64  `json:"updatedAt,omitempty"`
}

type Indexer struct {
	home       string
	mu         sync.Mutex
	idle       *sync.Cond
	dirty      bool
	running    bool
	inCatchUp  bool
	rebuilding bool
	closed     bool
	lastMeta   Meta
	lastErr    error
	wg         sync.WaitGroup
	stopCtx    context.Context
	stop       context.CancelFunc
}

func Rebuild(home string) (Meta, error) {
	idx, err := OpenIndexer(home)
	if err != nil {
		return Meta{DBPath: filepath.Join(strings.TrimSpace(home), dbName), LastError: err.Error()}, err
	}
	defer idx.Close()
	return idx.Rebuild()
}

func CatchUp(home string) (Meta, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return Meta{LastError: "home is required"}, fmt.Errorf("home is required")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return Meta{DBPath: filepath.Join(home, dbName), LastError: err.Error()}, err
	}
	return catchUpHome(home)
}

func Status(home string) Meta {
	home = strings.TrimSpace(home)
	meta := Meta{DBPath: filepath.Join(home, dbName)}
	if home == "" {
		meta.LastError = "home is required"
		return meta
	}
	if _, err := os.Stat(meta.DBPath); err != nil {
		return decorateLive(home, meta)
	}
	db, err := openDB(meta.DBPath, dbReadOnly)
	if err != nil {
		meta.LastError = "request-history index is unreadable"
		meta.RebuildRequired = true
		return decorateLive(home, meta)
	}
	defer closeDB(db)
	_, loaded, err := classifyDB(db.Query, db.QueryRow)
	if err != nil {
		meta.LastError = "request-history index is unreadable"
		meta.RebuildRequired = true
		return decorateLive(home, meta)
	}
	meta = loaded.meta(meta.DBPath)
	return decorateLive(home, meta)
}

func Lookup(home, id string) (map[string]any, error) {
	return lookupFresh(home, id, nil)
}

func OpenIndexer(home string) (*Indexer, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return nil, fmt.Errorf("home is required")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	idx := &Indexer{home: home, stopCtx: ctx, stop: cancel}
	idx.idle = sync.NewCond(&idx.mu)
	return idx, nil
}

func (idx *Indexer) Close() error {
	if idx == nil {
		return nil
	}
	idx.mu.Lock()
	if idx.closed {
		idx.mu.Unlock()
		return nil
	}
	idx.closed = true
	stop := idx.stop
	idx.idle.Broadcast()
	idx.mu.Unlock()
	if stop != nil {
		stop()
	}
	testHookMu.Lock()
	hookStop := testHookAfterIndexerStop
	testHookMu.Unlock()
	if hook := hookStop; hook != nil {
		hook()
	}
	idx.wg.Wait()
	return nil
}

func (idx *Indexer) CatchUp() (Meta, error) {
	if idx == nil {
		return Meta{}, fmt.Errorf("indexer is closed")
	}
	if err := idx.CatchUpBestEffort(); err != nil {
		return Meta{}, err
	}
	idx.mu.Lock()
	for !idx.closed && (idx.dirty || idx.inCatchUp) {
		idx.idle.Wait()
	}
	meta, err := idx.lastMeta, idx.lastErr
	idx.mu.Unlock()
	return meta, err
}

func (idx *Indexer) CatchUpBestEffort() error {
	if idx == nil {
		return nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.closed {
		return nil
	}
	idx.dirty = true
	if !idx.running {
		idx.running = true
		idx.wg.Add(1)
		go idx.worker()
	}
	idx.idle.Broadcast()
	return nil
}

func (idx *Indexer) Lookup(id string) (map[string]any, error) {
	return lookupFresh(idx.home, id, idx)
}

func (idx *Indexer) Status() Meta {
	if idx == nil {
		return Meta{}
	}
	return Status(idx.home)
}

func (idx *Indexer) Rebuild() (Meta, error) {
	if idx == nil {
		return Meta{}, fmt.Errorf("indexer is closed")
	}
	idx.mu.Lock()
	if idx.closed {
		idx.mu.Unlock()
		return Meta{}, fmt.Errorf("indexer is closed")
	}
	idx.rebuilding = true
	for idx.inCatchUp {
		idx.idle.Wait()
	}
	idx.mu.Unlock()
	meta, err := rebuildHome(idx.home)
	idx.mu.Lock()
	idx.lastMeta, idx.lastErr = meta, err
	idx.rebuilding = false
	idx.idle.Broadcast()
	idx.mu.Unlock()
	return meta, err
}

func (idx *Indexer) worker() {
	defer func() {
		idx.mu.Lock()
		idx.running = false
		idx.inCatchUp = false
		idx.idle.Broadcast()
		idx.mu.Unlock()
		idx.wg.Done()
	}()
	for {
		idx.mu.Lock()
		for !idx.dirty && !idx.closed {
			idx.idle.Wait()
		}
		if idx.closed && !idx.dirty {
			idx.mu.Unlock()
			return
		}
		for idx.rebuilding && !idx.closed {
			idx.idle.Wait()
		}
		if idx.closed && !idx.dirty {
			idx.mu.Unlock()
			return
		}
		idx.dirty = false
		idx.inCatchUp = true
		idx.mu.Unlock()
		meta, err := catchUpHomeCtx(idx.stopCtx, idx.home)
		if errors.Is(err, ErrRebuildRequired) && schemaUpgradeRebuildCandidate(meta) {
			meta, err = rebuildHome(idx.home)
		}
		idx.mu.Lock()
		idx.lastMeta, idx.lastErr = meta, err
		idx.inCatchUp = false
		idx.idle.Broadcast()
		idx.mu.Unlock()
	}
}

func catchUpHome(home string) (Meta, error) {
	return catchUpHomeCtx(context.Background(), home)
}

func catchUpHomeCtx(ctx context.Context, home string) (Meta, error) {
	lock, err := acquireWriterContext(ctx, home)
	if err != nil {
		meta := Meta{DBPath: filepath.Join(home, dbName), LastError: err.Error()}
		return decorateLive(home, meta), err
	}
	defer lock.Close()
	return catchUpLocked(home)
}

func catchUpLocked(home string) (Meta, error) {
	dbPath := filepath.Join(home, dbName)
	_, statErr := os.Stat(dbPath)
	freshFile := statErr != nil
	openMode := dbReadWrite
	if freshFile {
		openMode = dbCreate
	}
	db, err := openDB(dbPath, openMode)
	if err != nil {
		meta := Meta{DBPath: dbPath, LastError: err.Error(), RebuildRequired: true}
		return decorateLive(home, meta), fmt.Errorf("%w: %v", ErrRebuildRequired, err)
	}
	defer closeDB(db)
	if !freshFile {
		kind, state, err := classifyDB(db.Query, db.QueryRow)
		if err != nil {
			meta := Meta{DBPath: dbPath, LastError: err.Error(), RebuildRequired: true}
			return decorateLive(home, meta), fmt.Errorf("%w: %v", ErrRebuildRequired, err)
		}
		if kind == dbInvalid {
			meta := state.meta(dbPath)
			meta.RebuildRequired = true
			if meta.LastError == "" {
				meta.LastError = ErrRebuildRequired.Error()
			}
			return decorateLive(home, meta), ErrRebuildRequired
		}
		if kind == dbFresh {
			if err := withBusyRetry(func() error { return ensureSchema(db) }); err != nil {
				meta := Meta{DBPath: dbPath, LastError: err.Error()}
				return decorateLive(home, meta), err
			}
		}
	} else if err := withBusyRetry(func() error { return ensureSchema(db) }); err != nil {
		meta := Meta{DBPath: dbPath, LastError: err.Error()}
		return decorateLive(home, meta), err
	}
	return catchUp(db, home)
}

func uniqueRebuildPath(dbPath string) string {
	return fmt.Sprintf("%s.rebuild.%d.%d", dbPath, os.Getpid(), rebuildSeq.Add(1))
}

func rebuildHome(home string) (Meta, error) {
	dbPath := filepath.Join(home, dbName)
	tmpPath := uniqueRebuildPath(dbPath)
	removeDBFiles(tmpPath)
	db, err := openDB(tmpPath, dbCreate)
	if err != nil {
		removeDBFiles(tmpPath)
		return Meta{DBPath: dbPath, LastError: err.Error()}, err
	}
	installed := false
	defer func() {
		closeDB(db)
		if !installed {
			removeDBFiles(tmpPath)
		}
	}()
	if err := withBusyRetry(func() error { return ensureSchema(db) }); err != nil {
		return Meta{DBPath: dbPath, LastError: err.Error()}, err
	}
	meta, err := catchUp(db, home)
	meta.DBPath = dbPath
	if err != nil {
		return meta, err
	}
	if err := prepareInstallable(db); err != nil {
		return Meta{DBPath: dbPath, LastError: err.Error()}, err
	}
	closeDB(db)
	db = nil
	removeSidecars(tmpPath)
	// Test hook runs before the writer lock so concurrent rebuilds can finish
	// installing a newer generation while an older rebuild is held at install.
	testHookMu.Lock()
	hookInstall := testHookInstall
	testHookMu.Unlock()
	if hook := hookInstall; hook != nil {
		if err := hook(tmpPath, dbPath); err != nil {
			return meta, err
		}
	}
	lock, err := acquireWriter(home)
	if err != nil {
		return meta, err
	}
	defer lock.Close()
	// Final bounded catch-up/revalidation under the writer lock against the
	// current durable ledger boundary. New bytes after this snapshot belong
	// to the next CatchUp; the long initial rebuild does not hold the lock.
	meta, err = refreshRebuildCandidate(tmpPath, home)
	if err != nil {
		return meta, err
	}
	installed, err = installIndex(tmpPath, dbPath, home, meta.IndexedOffset)
	if err != nil {
		return meta, err
	}
	if !installed {
		removeDBFiles(tmpPath)
		return decorateLive(home, Status(home)), nil
	}
	return decorateLive(home, meta), nil
}

func closeDB(db *sql.DB) {
	if db == nil {
		return
	}
	_ = db.Close()
}

func removeDBFiles(path string) {
	_ = os.Remove(path)
	removeSidecars(path)
}

func removeSidecars(path string) {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_ = os.Remove(path + suffix)
	}
}

func refreshRebuildCandidate(tmp, home string) (Meta, error) {
	dbPath := filepath.Join(home, dbName)
	db, err := openDB(tmp, dbReadWrite)
	if err != nil {
		return Meta{DBPath: dbPath, LastError: err.Error()}, err
	}
	meta, err := catchUp(db, home)
	meta.DBPath = dbPath
	if err != nil {
		closeDB(db)
		return meta, err
	}
	if err := prepareInstallable(db); err != nil {
		closeDB(db)
		return Meta{DBPath: dbPath, LastError: err.Error()}, err
	}
	closeDB(db)
	removeSidecars(tmp)
	return meta, nil
}

func installIndex(tmp, dest, home string, candidateOffset int64) (bool, error) {
	authSize, err := authoritativeLedgerSize(home)
	if err != nil {
		return false, err
	}
	if candidateOffset > authSize {
		return false, fmt.Errorf("rebuild candidate is ahead of the durable ledger")
	}
	installedOffset, installedOK := installedCheckpoint(dest, authSize)
	// Only a strictly newer checkpoint that is valid against the same durable
	// ledger authority may suppress install. Equal offset must not suppress an
	// explicit rebuild: the candidate restores row contents from the ledger even
	// when schema/meta look healthy. An ahead/corrupt offset is never newer.
	if installedOK && installedOffset > candidateOffset {
		return false, nil
	}
	if err := replaceWithHook(tmp, dest); err != nil {
		return false, err
	}
	// Clear destination recovery sidecars only after replace succeeds so a
	// failed install leaves the previous DB recoverable.
	removeSidecars(dest)
	removeSidecars(tmp)
	return true, nil
}

func replaceWithHook(tmp, dest string) error {
	testHookMu.Lock()
	hookReplace := testHookReplace
	testHookMu.Unlock()
	if hook := hookReplace; hook != nil {
		if err := hook(tmp, dest); err != nil {
			return err
		}
	}
	return replaceFile(tmp, dest)
}

func authoritativeLedgerSize(home string) (int64, error) {
	ledger, err := usageledger.Open(home)
	if err != nil {
		return 0, err
	}
	return ledger.LogicalSize()
}

func installedCheckpoint(dest string, authSize int64) (int64, bool) {
	if _, err := os.Stat(dest); err != nil {
		return 0, false
	}
	db, err := openDB(dest, dbReadOnly)
	if err != nil {
		return 0, false
	}
	defer closeDB(db)
	kind, state, err := classifyDB(db.Query, db.QueryRow)
	if err != nil || kind != dbUsable {
		return 0, false
	}
	if state.indexedOffset > authSize {
		return state.indexedOffset, false
	}
	return state.indexedOffset, true
}

func prepareInstallable(db *sql.DB) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`PRAGMA journal_mode = DELETE; PRAGMA wal_checkpoint(TRUNCATE);`)
	return err
}

func lookupFresh(home, id string, idx *Indexer) (map[string]any, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, os.ErrNotExist
	}
	st := Status(home)
	if st.RebuildRequired {
		return nil, ErrRebuildRequired
	}
	row, err := lookupOnly(home, id)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !os.IsNotExist(err) {
		return nil, ErrUnavailable
	}
	if st.CaughtUp {
		return nil, os.ErrNotExist
	}
	if idx != nil {
		_ = idx.CatchUpBestEffort()
	}
	return nil, ErrUnavailable
}

func lookupOnly(home, id string) (map[string]any, error) {
	dbPath := filepath.Join(strings.TrimSpace(home), dbName)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, os.ErrNotExist
	}
	db, err := openDB(dbPath, dbReadOnly)
	if err != nil {
		return nil, err
	}
	defer closeDB(db)
	return scanRequest(db, id)
}

func scanRequest(db *sql.DB, id string) (map[string]any, error) {
	var raw string
	err := db.QueryRow(`SELECT row_json FROM requests WHERE request_id = ?`, id).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var row map[string]any
	if json.Unmarshal([]byte(raw), &row) != nil {
		return nil, fmt.Errorf("row is not JSON")
	}
	return row, nil
}

func catchUp(db *sql.DB, home string) (Meta, error) {
	meta := Meta{SchemaVersion: schemaVersion, DBPath: filepath.Join(home, dbName)}
	testHookMu.Lock()
	hookHold := testHookHoldCatchUp
	testHookMu.Unlock()
	if hook := hookHold; hook != nil {
		hook()
	}
	testHookMu.Lock()
	hookBefore := testHookBeforeCatchUp
	testHookMu.Unlock()
	if hook := hookBefore; hook != nil {
		if err := hook(); err != nil {
			meta.LastError = err.Error()
			return decorateLive(home, meta), err
		}
	}
	var tx *sql.Tx
	if err := withBusyRetry(func() error {
		started, err := db.Begin()
		if err != nil {
			return err
		}
		tx = started
		return nil
	}); err != nil {
		meta.LastError = err.Error()
		return decorateLive(home, meta), err
	}
	state, err := readStateTx(tx, true)
	if err != nil {
		_ = tx.Rollback()
		meta.LastError = err.Error()
		meta.RebuildRequired = true
		return persistError(db, decorateLive(home, meta), err)
	}
	applyRowConsistency(&state)
	if err := applySchemaShape(tx.Query, tx.QueryRow, &state); err != nil {
		state.rebuildRequired = true
		state.trustworthy = false
		state.lastError = err.Error()
	}
	meta = state.meta(meta.DBPath)
	if state.rebuildRequired || !state.trustworthy {
		_ = tx.Rollback()
		meta.RebuildRequired = true
		if meta.LastError == "" {
			meta.LastError = ErrRebuildRequired.Error()
		}
		return decorateLive(home, meta), ErrRebuildRequired
	}
	ledger, err := usageledger.Open(home)
	if err != nil {
		_ = tx.Rollback()
		meta.LastError = err.Error()
		return persistError(db, decorateLive(home, meta), err)
	}
	live, err := ledger.LogicalSize()
	if err != nil {
		_ = tx.Rollback()
		meta.LastError = err.Error()
		if errors.Is(err, usageledger.ErrCorruptLedger) {
			return persistError(db, decorateLive(home, meta), err)
		}
		meta.RebuildRequired = true
		return persistError(db, decorateLive(home, meta), err)
	}
	if state.indexedOffset > live {
		meta.RebuildRequired = true
		meta.LastError = "indexed offset is ahead of the durable ledger"
		if err := writeMetaTx(tx, meta); err != nil {
			_ = tx.Rollback()
			return decorateLive(home, meta), err
		}
		if err := tx.Commit(); err != nil {
			return decorateLive(home, meta), err
		}
		return decorateLive(home, meta), ErrRebuildRequired
	}
	ext, extErr := ledger.Extents()
	if extErr != nil {
		_ = tx.Rollback()
		meta.LastError = extErr.Error()
		return persistError(db, decorateLive(home, meta), extErr)
	}
	start := state.indexedOffset
	if start < ext.RetainedFromOffset {
		start = ext.RetainedFromOffset
	}
	insert, err := tx.Prepare(`INSERT INTO requests(request_id, row_json, ledger_offset) VALUES (?, ?, ?) ON CONFLICT(request_id) DO NOTHING`)
	if err != nil {
		_ = tx.Rollback()
		meta.LastError = err.Error()
		return persistError(db, decorateLive(home, meta), err)
	}
	defer insert.Close()
	update, err := tx.Prepare(`UPDATE requests SET row_json = ?, ledger_offset = ? WHERE request_id = ?`)
	if err != nil {
		_ = tx.Rollback()
		meta.LastError = err.Error()
		return persistError(db, decorateLive(home, meta), err)
	}
	defer update.Close()
	indexedRows := state.indexedRows
	if state.retentionWatermark < ext.RetainedFromOffset {
		if _, delErr := tx.Exec(`DELETE FROM requests WHERE ledger_offset < ?`, ext.RetainedFromOffset); delErr != nil {
			_ = tx.Rollback()
			meta.LastError = delErr.Error()
			return persistError(db, decorateLive(home, meta), delErr)
		}
		if err := tx.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&indexedRows); err != nil {
			_ = tx.Rollback()
			meta.LastError = err.Error()
			return persistError(db, decorateLive(home, meta), err)
		}
	}
	end, err := ledger.EnumerateFromSnapshot(start, func(rec usageledger.Record) error {
		if rec.Oversized {
			return nil
		}
		id, text, ok := parseRequestRow(rec.Line)
		if !ok {
			return nil
		}
		res, err := insert.Exec(id, text, rec.Offset)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 1 {
			indexedRows++
			return nil
		}
		_, err = update.Exec(text, rec.Offset, id)
		return err
	})
	if err != nil {
		_ = tx.Rollback()
		meta.LastError = err.Error()
		if errors.Is(err, usageledger.ErrUnalignedOffset) || errors.Is(err, usageledger.ErrOffsetBeyondEnd) {
			meta.RebuildRequired = true
			return persistError(db, decorateLive(home, meta), err)
		}
		return persistError(db, decorateLive(home, meta), err)
	}
	testHookMu.Lock()
	hookAfter := testHookAfterRows
	testHookMu.Unlock()
	if hook := hookAfter; hook != nil {
		if hookErr := hook(); hookErr != nil {
			_ = tx.Rollback()
			return decorateLive(home, meta), hookErr
		}
	}
	meta.SchemaVersion = schemaVersion
	meta.SourceSize = end
	meta.LedgerEndOffset = end
	meta.RetentionWatermark = ext.RetainedFromOffset
	meta.IndexedOffset = end
	meta.IndexedRows = indexedRows
	meta.LastError = ""
	meta.RebuildRequired = false
	meta.UpdatedAtMs = time.Now().UnixMilli()
	if err := writeMetaTx(tx, meta); err != nil {
		_ = tx.Rollback()
		return decorateLive(home, meta), err
	}
	testHookMu.Lock()
	hookBeforeCommit := testHookBeforeCommit
	testHookMu.Unlock()
	if hook := hookBeforeCommit; hook != nil {
		if hookErr := hook(); hookErr != nil {
			_ = tx.Rollback()
			return decorateLive(home, meta), hookErr
		}
	}
	if err := tx.Commit(); err != nil {
		meta.LastError = err.Error()
		return decorateLive(home, meta), err
	}
	return decorateLive(home, meta), nil
}

func persistError(db *sql.DB, meta Meta, cause error) (Meta, error) {
	tx, err := db.Begin()
	if err != nil {
		return meta, cause
	}
	if err := writeMetaTx(tx, meta); err != nil {
		_ = tx.Rollback()
		return meta, cause
	}
	if err := tx.Commit(); err != nil {
		return meta, cause
	}
	return meta, cause
}

func parseRequestRow(line []byte) (string, string, bool) {
	text := strings.TrimSpace(string(line))
	if text == "" {
		return "", "", false
	}
	var row map[string]any
	if json.Unmarshal([]byte(text), &row) != nil {
		return "", "", false
	}
	id, _ := row["requestId"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", "", false
	}
	return id, text, true
}

type dbOpenMode int

const (
	dbReadOnly dbOpenMode = iota
	dbReadWrite
	dbCreate
)

func openDB(path string, mode dbOpenMode) (*sql.DB, error) {
	if mode != dbCreate {
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
	}
	dsn := sqliteDSN(path, mode)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := withBusyRetry(func() error {
		_, err := db.Exec(`PRAGMA busy_timeout = 5000; PRAGMA journal_mode = DELETE`)
		return err
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func sqliteDSN(path string, mode dbOpenMode) string {
	slash := filepath.ToSlash(path)
	if len(slash) >= 2 && slash[1] == ':' {
		slash = "/" + slash
	}
	dsn := "file://" + slash + "?_pragma=busy_timeout(5000)&_txlock=immediate"
	switch mode {
	case dbCreate:
		return dsn + "&mode=rwc"
	case dbReadWrite:
		return dsn + "&mode=rw"
	default:
		return dsn + "&mode=ro"
	}
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "SQLITE_BUSY") || strings.Contains(s, "database is locked")
}

func withBusyRetry(fn func() error) error {
	var err error
	for i := 0; i < 80; i++ {
		err = fn()
		if err == nil || !isBusy(err) {
			return err
		}
		time.Sleep(time.Duration(10+i) * time.Millisecond)
	}
	return err
}

func ensureSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS requests (
  request_id TEXT PRIMARY KEY,
  row_json TEXT NOT NULL,
  ledger_offset INTEGER NOT NULL
);`)
	return err
}

func schemaUpgradeRebuildCandidate(meta Meta) bool {
	// v1/v2 indexes and shape-invalid v2 files are upgraded by one background rebuild.
	if meta.SchemaVersion == 1 || meta.SchemaVersion == 2 {
		return true
	}
	return meta.RebuildRequired && meta.SchemaVersion == 0
}

type dbState struct {
	schemaVersion      int
	indexedOffset      int64
	sourceSize         int64
	ledgerEndOffset    int64
	retentionWatermark int64
	indexedRows        int
	lastError          string
	updatedAtMs        int64
	rebuildRequired    bool
	hasOffset          bool
	hasIndexedRows     bool
	seenVersion        bool
	hasRequests        bool
	trustworthy        bool
}

func (s dbState) meta(dbPath string) Meta {
	end := s.ledgerEndOffset
	if end == 0 {
		end = s.sourceSize
	}
	return Meta{
		SchemaVersion:      s.schemaVersion,
		DBPath:             dbPath,
		SourceSize:         s.sourceSize,
		LedgerEndOffset:    end,
		RetentionWatermark: s.retentionWatermark,
		IndexedOffset:      s.indexedOffset,
		IndexedRows:        s.indexedRows,
		LastError:          s.lastError,
		RebuildRequired:    s.rebuildRequired,
		UpdatedAtMs:        s.updatedAtMs,
	}
}

func readState(db *sql.DB, allowCount bool) (dbState, error) {
	return scanState(func() (*sql.Rows, error) {
		return db.Query(`SELECT key, value FROM schema_meta`)
	}, func() (bool, error) {
		return requestsExist(db.QueryRow)
	}, func() (int, error) {
		return countRequests(db.QueryRow)
	}, allowCount)
}

func readStateTx(tx *sql.Tx, allowCount bool) (dbState, error) {
	return scanState(func() (*sql.Rows, error) {
		return tx.Query(`SELECT key, value FROM schema_meta`)
	}, func() (bool, error) {
		return requestsExist(tx.QueryRow)
	}, func() (int, error) {
		return countRequests(tx.QueryRow)
	}, allowCount)
}

func requestsExist(queryRow func(string, ...any) *sql.Row) (bool, error) {
	var one int
	err := queryRow(`SELECT 1 FROM requests LIMIT 1`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func countRequests(queryRow func(string, ...any) *sql.Row) (int, error) {
	testHookMu.Lock()
	hookCount := testHookCountTable
	testHookMu.Unlock()
	if hook := hookCount; hook != nil {
		hook()
	}
	var n int
	err := queryRow(`SELECT COUNT(*) FROM requests`).Scan(&n)
	return n, err
}

func scanState(query func() (*sql.Rows, error), exists func() (bool, error), count func() (int, error), allowCount bool) (dbState, error) {
	rows, err := query()
	if err != nil {
		return dbState{}, err
	}
	defer rows.Close()
	var state dbState
	malformed := ""
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return dbState{}, err
		}
		switch key {
		case "schema_version":
			state.seenVersion = true
			n, err := parseNonNegInt(value, "schema_version")
			if err != nil {
				malformed = err.Error()
				continue
			}
			state.schemaVersion = n
		case "source_size":
			n, err := parseNonNegInt64(value, "source_size")
			if err != nil {
				malformed = err.Error()
				continue
			}
			state.sourceSize = n
		case "ledger_end_offset":
			n, err := parseNonNegInt64(value, "ledger_end_offset")
			if err != nil {
				malformed = err.Error()
				continue
			}
			state.ledgerEndOffset = n
		case "retention_watermark":
			n, err := parseNonNegInt64(value, "retention_watermark")
			if err != nil {
				malformed = err.Error()
				continue
			}
			state.retentionWatermark = n
		case "indexed_offset":
			n, err := parseNonNegInt64(value, "indexed_offset")
			if err != nil {
				malformed = err.Error()
				state.hasOffset = true
				continue
			}
			state.indexedOffset = n
			state.hasOffset = true
		case "indexed_rows":
			n, err := parseNonNegInt(value, "indexed_rows")
			if err != nil {
				malformed = err.Error()
				continue
			}
			state.indexedRows = n
			state.hasIndexedRows = true
		case "last_error":
			state.lastError = value
		case "built_at_ms", "updated_at_ms":
			n, err := parseNonNegInt64(value, key)
			if err != nil {
				continue
			}
			state.updatedAtMs = n
		case "rebuild_required":
			flag, err := parseRebuildRequired(value)
			if err != nil {
				malformed = err.Error()
				continue
			}
			state.rebuildRequired = flag
		}
	}
	if err := rows.Err(); err != nil {
		return dbState{}, err
	}
	hasRows, err := exists()
	if err != nil {
		return dbState{}, err
	}
	state.hasRequests = hasRows
	if malformed != "" {
		state.rebuildRequired = true
		state.trustworthy = false
		state.lastError = malformed
		return state, nil
	}
	switch {
	case state.rebuildRequired:
		state.trustworthy = false
	case state.seenVersion && state.schemaVersion > schemaVersion:
		state.rebuildRequired = true
		state.lastError = "incompatible request-history schema"
	case state.seenVersion && state.schemaVersion != 1 && state.schemaVersion != schemaVersion:
		state.rebuildRequired = true
		state.lastError = "request-history schema is invalid"
	case !state.seenVersion && !state.hasOffset && !hasRows:
		state.trustworthy = true
		state.schemaVersion = schemaVersion
		state.indexedRows = 0
	case !state.seenVersion && hasRows:
		state.rebuildRequired = true
		state.lastError = "request-history checkpoint is missing"
	case state.schemaVersion == 1 && state.hasOffset && state.lastError != "":
		state.rebuildRequired = true
	case state.schemaVersion == 1 && state.hasOffset:
		state.trustworthy = true
		if allowCount {
			n, err := count()
			if err != nil {
				return dbState{}, err
			}
			state.indexedRows = n
			state.hasIndexedRows = true
		}
	case state.schemaVersion == schemaVersion && state.hasOffset && state.hasIndexedRows:
		state.trustworthy = true
		applyRowConsistency(&state)
	case state.schemaVersion == schemaVersion && state.hasOffset && !state.hasIndexedRows:
		state.rebuildRequired = true
		state.lastError = "request-history row count is missing"
	case !state.hasOffset && hasRows:
		state.rebuildRequired = true
		state.lastError = "request-history checkpoint is missing"
	default:
		state.rebuildRequired = true
		if state.lastError == "" {
			state.lastError = "request-history index is unusable"
		}
	}
	return state, nil
}

func applySchemaShape(query func(string, ...any) (*sql.Rows, error), queryRow func(string, ...any) *sql.Row, state *dbState) error {
	if state == nil {
		return fmt.Errorf("index state is missing")
	}
	return validateExactSchema(query, queryRow)
}

type colNeed struct {
	name    string
	pk      bool
	notnull bool
	text    bool
	integer bool
}

func parseNonNegInt(value, name string) (int, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return 0, fmt.Errorf("%s is malformed", name)
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s is malformed", name)
	}
	if n < 0 {
		return 0, fmt.Errorf("%s is negative", name)
	}
	return n, nil
}

func parseNonNegInt64(value, name string) (int64, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return 0, fmt.Errorf("%s is malformed", name)
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s is malformed", name)
	}
	if n < 0 {
		return 0, fmt.Errorf("%s is negative", name)
	}
	return n, nil
}

func parseRebuildRequired(value string) (bool, error) {
	switch value {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("rebuild_required is malformed")
	}
}

func writeMetaTx(tx *sql.Tx, meta Meta) error {
	rebuild := "0"
	if meta.RebuildRequired {
		rebuild = "1"
	}
	schema := meta.SchemaVersion
	if schema == 0 {
		schema = schemaVersion
	}
	updated := meta.UpdatedAtMs
	if updated == 0 {
		updated = time.Now().UnixMilli()
	}
	end := meta.LedgerEndOffset
	if end == 0 {
		end = meta.SourceSize
	}
	pairs := map[string]string{
		"schema_version":      strconv.Itoa(schema),
		"source_size":         strconv.FormatInt(meta.SourceSize, 10),
		"ledger_end_offset":   strconv.FormatInt(end, 10),
		"retention_watermark": strconv.FormatInt(meta.RetentionWatermark, 10),
		"indexed_offset":      strconv.FormatInt(meta.IndexedOffset, 10),
		"indexed_rows":        strconv.Itoa(meta.IndexedRows),
		"built_at_ms":         strconv.FormatInt(time.Now().UnixMilli(), 10),
		"updated_at_ms":       strconv.FormatInt(updated, 10),
		"last_error":          meta.LastError,
		"rebuild_required":    rebuild,
	}
	for key, value := range pairs {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO schema_meta(key, value) VALUES (?, ?)`, key, value); err != nil {
			return err
		}
	}
	return nil
}

func decorateLive(home string, meta Meta) Meta {
	ledger, err := usageledger.Open(home)
	if err != nil {
		if meta.LastError == "" {
			meta.LastError = err.Error()
		}
		meta.CaughtUp = false
		return meta
	}
	ext, err := ledger.Extents()
	if err != nil {
		if meta.LastError == "" {
			meta.LastError = err.Error()
		}
		meta.CaughtUp = false
		if errors.Is(err, usageledger.ErrCorruptLedger) && meta.IndexedOffset > 0 {
			meta.RebuildRequired = true
		}
		return meta
	}
	size := ext.EndOffset
	meta.SourceSize = size
	meta.LedgerEndOffset = size
	if meta.RetentionWatermark < ext.RetainedFromOffset {
		meta.RetentionWatermark = ext.RetainedFromOffset
	}
	if meta.RebuildRequired {
		meta.CaughtUp = false
		if meta.IndexedOffset <= size {
			meta.PendingBytes = size - meta.IndexedOffset
		}
		return meta
	}
	if meta.IndexedOffset > size {
		meta.RebuildRequired = true
		meta.CaughtUp = false
		if meta.LastError == "" {
			meta.LastError = "indexed offset is ahead of the durable ledger"
		}
		return meta
	}
	meta.PendingBytes = size - meta.IndexedOffset
	meta.CaughtUp = meta.PendingBytes == 0 && meta.LastError == ""
	return meta
}
