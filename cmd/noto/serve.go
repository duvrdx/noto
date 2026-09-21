package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/duvrdx/noto/internal/platform/config"
)

const (
	// httpAddr é a porta do serve: constante por decisão do usuário, não
	// variável de ambiente (a spec configuration fixa o conjunto do .env.example).
	httpAddr = ":8080"

	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// runServe é o modo serve: recebe do Telegram por long polling, persiste e
// responde (serveIngest), e atende GET /healthz em httpAddr. Só o transporte
// polling é suportado neste marco; o webhook é recusado antes de qualquer
// E/S (banco, rede ou porta). Só retorna quando o contexto cai (nil) ou algo
// falha. Não depende do worker.
func runServe(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	if cfg.TelegramTransport != config.TransportPolling {
		return fmt.Errorf("TELEGRAM_TRANSPORT=%q não é suportado neste marco: só %q por enquanto",
			cfg.TelegramTransport, config.TransportPolling)
	}
	ln, err := net.Listen("tcp", httpAddr)
	if err != nil {
		return fmt.Errorf("escutar em %s: %w", httpAddr, err)
	}
	// Se a subida falhar antes de o servidor HTTP assumir o listener, ele não vaza.
	defer ln.Close()
	return serveIngest(ctx, cfg, ln, "", log)
}

// newServeMux devolve as rotas do serve, com o ServeMux da stdlib (ADR 0001).
func newServeMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// serve atende em ln até o contexto ser cancelado; então faz Shutdown
// gracioso e devolve nil. Erro de servidor antes disso é devolvido.
func serve(ctx context.Context, ln net.Listener, log *slog.Logger) error {
	srv := &http.Server{
		Handler:           newServeMux(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("servidor HTTP escutando", "addr", ln.Addr().String())
		errc <- srv.Serve(ln)
	}()

	select {
	case err := <-errc:
		// Serve só retorna sem Shutdown se falhou (ErrServerClosed não ocorre aqui).
		return fmt.Errorf("servidor HTTP: %w", err)
	case <-ctx.Done():
	}

	log.Info("encerrando servidor HTTP")
	// o ctx já caiu; o Shutdown precisa de um prazo próprio
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown do servidor HTTP: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("servidor HTTP: %w", err)
	}
	return nil
}
