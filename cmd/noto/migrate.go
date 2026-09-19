package main

import (
	"context"
	"log/slog"

	"github.com/duvrdx/noto/internal/platform/config"
	"github.com/duvrdx/noto/internal/platform/migrations"
)

// runMigrate aplica as migrações pendentes contra DATABASE_URL. Nunca roda
// sozinho no start do serve/worker: é um passo explícito (design.md §5).
func runMigrate(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	return migrations.Up(ctx, cfg.DatabaseURL, log)
}
