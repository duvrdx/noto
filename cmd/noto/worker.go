package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/duvrdx/noto/internal/platform/config"
)

// tickInterval é o intervalo do laço do worker. Ainda não há trabalho a
// fazer; o valor só importa quando a fila chegar (M4).
const tickInterval = 10 * time.Second

// runWorker roda o laço de tick até o contexto cair. Não depende do serve:
// toda coordenação entre os dois passará pelo PostgreSQL.
func runWorker(ctx context.Context, _ config.Config, log *slog.Logger) error {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	return workerLoop(ctx, ticker.C, log)
}

// workerLoop espera entre o cancelamento do contexto e o tick. O tick é um
// canal injetado para os testes não dependerem de relógio. Por ora o tick é
// vazio e só loga; nenhuma lógica de fila (isso é o M4).
func workerLoop(ctx context.Context, tick <-chan time.Time, log *slog.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
			log.Debug("tick")
		}
	}
}
