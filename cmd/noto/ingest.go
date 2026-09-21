package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/duvrdx/noto/internal/adapters/postgres"
	"github.com/duvrdx/noto/internal/adapters/telegram"
	"github.com/duvrdx/noto/internal/app"
	"github.com/duvrdx/noto/internal/core/message"
	"github.com/duvrdx/noto/internal/platform/config"
)

// serveIngest é a subida do serve em modo polling, na ordem: banco (pool com
// Ping) -> Telegram (o getMe valida o token) -> caso de uso -> polling e HTTP
// juntos (runIngest). O pool é fechado por último, depois de o polling e o
// servidor HTTP terminarem: um update em curso ainda precisa dele.
//
// apiBaseURL é da API do Telegram; vazio vale o de produção. Existe como
// parâmetro (e não como variável de ambiente) para o teste apontar ao fake.
//
// Os erros daqui chegam ao stderr só como mensagem (%v): esta função não
// extrai tipos da cadeia de erro para exibi-los, porque o erro original do
// cliente Telegram, ainda alcançável pela cadeia, pode ter o token.
func serveIngest(ctx context.Context, cfg config.Config, ln net.Listener, apiBaseURL string, log *slog.Logger) error {
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("abrir o banco: %w", err)
	}
	defer pool.Close()

	client, err := telegram.NewClient(telegram.Config{Token: cfg.TelegramBotToken, APIBaseURL: apiBaseURL, Log: log})
	if err != nil {
		return fmt.Errorf("iniciar o Telegram: %w", err)
	}
	return runIngest(ctx, ln, client, pool, log)
}

// runIngest monta o caso de uso e roda, num errgroup, o polling do Telegram e
// o servidor HTTP em ln: o erro de um cancela o outro e a função só retorna
// quando os dois terminaram (o polling conclui o update em curso, o HTTP faz
// Shutdown). Devolve nil quando ctx é cancelado. Quem chama fecha o pool
// depois.
func runIngest(ctx context.Context, ln net.Listener, client *telegram.Client, pool *pgxpool.Pool, log *slog.Logger) error {
	ingest := app.NewIngestMessage(postgres.NewMessageStore(pool), client.Notifier(), log)
	handler := func(ctx context.Context, in message.Incoming) error {
		_, err := ingest.Handle(ctx, in)
		return err
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		log.Info("polling do Telegram iniciado")
		return client.Run(gctx, handler)
	})
	g.Go(func() error { return serve(gctx, ln, log) })
	return g.Wait()
}
