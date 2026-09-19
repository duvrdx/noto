package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/duvrdx/noto/internal/platform/config"
)

// runWorker é um stub temporário: a task 4.4 o substitui pelo laço de tick.
func runWorker(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	return errors.New("worker: ainda não implementado (task 4.4)")
}
