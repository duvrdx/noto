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

// runServe sobe o servidor HTTP em httpAddr e só retorna quando o contexto
// cai (nil) ou o servidor falha. Não depende do worker.
func runServe(ctx context.Context, _ config.Config, log *slog.Logger) error {
	ln, err := net.Listen("tcp", httpAddr)
	if err != nil {
		return fmt.Errorf("escutar em %s: %w", httpAddr, err)
	}
	return serve(ctx, ln, log)
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
