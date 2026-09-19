package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/duvrdx/noto/internal/platform/config"
)

// runMigrate é um stub temporário, só para o despacho compilar e o
// subcomando ser reconhecido. A task 5.2 o substitui pela implementação
// real (goose como biblioteca sobre as migrações embutidas).
func runMigrate(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	return errors.New("migrate: ainda não implementado (task 5.2)")
}
