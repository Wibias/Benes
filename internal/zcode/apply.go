package zcode

import (
	"context"
	"errors"
	"os"

	"github.com/Wibias/Benes/internal/catalog"
)

const (
	clientName   = "zcode"
	artifactName = "config.json"
)

func Apply(ctx context.Context, origin string) error {
	return applyFragment(ctx, origin, nil)
}

func ApplyWithModels(ctx context.Context, origin string, models []catalog.Model) error {
	return applyFragment(ctx, origin, ModelSelectors(models))
}

func applyFragment(ctx context.Context, origin string, models map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fragment, err := ProviderFragment(origin)
	if err != nil {
		return err
	}
	if models != nil {
		fragment["models"] = models
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	body, err := MergeProvider(existing, fragment)
	if err != nil {
		return err
	}
	return commitConfig(ctx, path, body)
}
