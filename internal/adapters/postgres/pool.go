package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pingTimeout limita o teste de conexão na abertura, para um host que não
// responde não deixar o processo pendurado.
const pingTimeout = 10 * time.Second

// NewPool abre um pool pgx para dsn e confere a conexão com um Ping (sob
// timeout de 10 s): banco fora do ar falha na subida, não na primeira
// mensagem. Em erro devolve nil. Os erros não repetem o DSN; o pgx já mascara
// a senha ao rejeitar um DSN malformado, e os testes provam os dois caminhos.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL inválida: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("conectar ao banco: %w", err)
	}
	return pool, nil
}
