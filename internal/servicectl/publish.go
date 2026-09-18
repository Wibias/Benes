package servicectl

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/managedfs"
)

const clientName = "service-manager"

func PublishUnit(ctx context.Context, unitDir, name string, body []byte) error {
	if strings.TrimSpace(unitDir) == "" {
		return fmt.Errorf("service unit directory is required")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("service unit name is required")
	}
	coord, err := managedfs.New(unitDir)
	if err != nil {
		return err
	}
	tx, err := coord.Begin(ctx, clientName)
	if err != nil {
		return err
	}
	if err := tx.Stage(name, body); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
