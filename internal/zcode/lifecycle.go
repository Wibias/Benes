package zcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/managedfs"
)

func Disable(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	body, err := RemoveProvider(existing)
	if err != nil {
		return err
	}
	if err := commitConfig(ctx, path, body); err != nil {
		return err
	}
	coord, err := managedfs.New(filepath.Dir(path))
	if err != nil {
		return err
	}
	return coord.Disable(ctx)
}

func Enable(ctx context.Context, origin string, models []catalog.Model) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	coord, err := managedfs.New(filepath.Dir(path))
	if err != nil {
		return err
	}
	if coord.Classify() == managedfs.OwnershipDisabled {
		if err := coord.Enable(ctx); err != nil {
			return err
		}
	}
	return ApplyWithModels(ctx, origin, models)
}

func Recover(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	coord, err := managedfs.New(filepath.Dir(path))
	if err != nil {
		return err
	}
	return coord.Recover(ctx)
}

func RestoreHistory(ctx context.Context, generation int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	coord, err := managedfs.New(filepath.Dir(path))
	if err != nil {
		return err
	}
	return coord.Restore(ctx, generation)
}

func commitConfig(ctx context.Context, path string, body []byte) error {
	coord, err := managedfs.New(filepath.Dir(path))
	if err != nil {
		return err
	}
	tx, err := coord.BeginReassert(ctx, clientName)
	if err != nil {
		return err
	}
	if err := tx.Stage(artifactName, body); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
