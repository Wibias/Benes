package requesthistory

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/usageledger"
)

// ApplyRetention compacts the derived SQLite index after the ledger retention
// watermark advances. Ledger prune must already be committed; this runs under
// the PR181 writer lock and never imports the reverse dependency.
func ApplyRetention(home string) (Meta, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return Meta{}, fmt.Errorf("home is required")
	}
	lock, err := acquireWriter(home)
	if err != nil {
		return Meta{DBPath: filepath.Join(home, dbName), LastError: err.Error()}, err
	}
	defer lock.Close()
	return applyRetentionLocked(home)
}

func applyRetentionLocked(home string) (Meta, error) {
	ledger, err := usageledger.Open(home)
	if err != nil {
		return Meta{DBPath: filepath.Join(home, dbName), LastError: err.Error()}, err
	}
	ext, err := ledger.Extents()
	if err != nil {
		return Meta{DBPath: filepath.Join(home, dbName), LastError: err.Error()}, err
	}
	dbPath := filepath.Join(home, dbName)
	db, err := openDB(dbPath, dbReadWrite)
	if err != nil {
		meta := decorateLive(home, Meta{
			DBPath:             dbPath,
			SchemaVersion:      schemaVersion,
			RetentionWatermark: ext.RetainedFromOffset,
			LedgerEndOffset:    ext.EndOffset,
			SourceSize:         ext.EndOffset,
		})
		return meta, nil
	}
	defer closeDB(db)
	kind, state, err := classifyDB(db.Query, db.QueryRow)
	if err != nil {
		return decorateLive(home, Meta{DBPath: dbPath, LastError: err.Error(), RebuildRequired: true}), err
	}
	if kind != dbUsable {
		meta := state.meta(dbPath)
		meta.RebuildRequired = true
		return decorateLive(home, meta), ErrRebuildRequired
	}
	tx, err := db.Begin()
	if err != nil {
		meta := state.meta(dbPath)
		meta.LastError = err.Error()
		return decorateLive(home, meta), err
	}
	meta := state.meta(dbPath)
	if ext.RetainedFromOffset > meta.RetentionWatermark {
		if _, delErr := tx.Exec(`DELETE FROM requests WHERE ledger_offset < ?`, ext.RetainedFromOffset); delErr != nil {
			_ = tx.Rollback()
			meta.LastError = delErr.Error()
			return decorateLive(home, meta), delErr
		}
		if err := tx.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&meta.IndexedRows); err != nil {
			_ = tx.Rollback()
			meta.LastError = err.Error()
			return decorateLive(home, meta), err
		}
		meta.RetentionWatermark = ext.RetainedFromOffset
	}
	if meta.IndexedOffset < ext.RetainedFromOffset {
		meta.IndexedOffset = ext.RetainedFromOffset
	}
	meta.SchemaVersion = schemaVersion
	meta.SourceSize = ext.EndOffset
	meta.LedgerEndOffset = ext.EndOffset
	meta.LastError = ""
	meta.RebuildRequired = false
	if err := writeMetaTx(tx, meta); err != nil {
		_ = tx.Rollback()
		meta.LastError = err.Error()
		return decorateLive(home, meta), err
	}
	if err := tx.Commit(); err != nil {
		meta.LastError = err.Error()
		return decorateLive(home, meta), err
	}
	_, _ = db.Exec(`VACUUM`)
	return decorateLive(home, meta), nil
}
