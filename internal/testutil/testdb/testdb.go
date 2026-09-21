// Package testdb entrega Postgres real aos testes de integração, via
// testcontainers (docs/testing.md §3). Repositórios não são dublados.
//
// Um contêiner postgres:16 sobe por execução do binário de teste (lazy, na
// primeira chamada), rodando em UTC de propósito (testing.md §4), e as
// migrações do repositório são aplicadas uma vez pelo caminho real
// (internal/platform/migrations.Up). Tx dá a cada teste uma transação com
// rollback; Pool dá um banco novo, já migrado, para quem precisa de commit
// visível entre conexões.
//
// Com go test -short os testes que usam o helper são pulados. Sem -short e
// sem Docker eles falham, com mensagem que cita o Docker: um pulo silencioso
// faria a suíte mentir. O contêiner não é derrubado explicitamente; o
// reaper (Ryuk) do testcontainers o remove quando o processo de teste sai.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/duvrdx/noto/internal/platform/migrations"
)

const (
	image        = "postgres:16"
	startTimeout = 3 * time.Minute
	opTimeout    = 30 * time.Second
)

// shared é o contêiner do processo: o banco padrão migrado, que serve aos
// testes com Tx e às operações administrativas (CREATE/DROP DATABASE).
type shared struct {
	pool    *pgxpool.Pool
	connStr string // DSN do banco padrão
}

var (
	once     sync.Once
	instance *shared
	startErr error
)

// get sobe o contêiner na primeira chamada; as seguintes reaproveitam o
// resultado, inclusive a falha (não adianta repetir a tentativa em cada teste).
func get(t testing.TB) *shared {
	t.Helper()
	if testing.Short() {
		t.Skip("testdb: -short pula testes que exigem Docker")
	}
	once.Do(func() { instance, startErr = start() })
	if startErr != nil {
		t.Fatalf("testdb: Docker é necessário para os testes de banco (use go test -short para pulá-los): %v", startErr)
	}
	return instance
}

func start() (s *shared, err error) {
	// O testcontainers pode entrar em pânico em ambientes sem daemon; um
	// pânico aqui viraria falha ilegível de todo o binário de teste.
	defer func() {
		if r := recover(); r != nil {
			s, err = nil, fmt.Errorf("pânico ao iniciar o contêiner: %v", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()

	ctr, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("noto_test"),
		tcpostgres.WithUsername("noto"),
		tcpostgres.WithPassword("noto"),
		// UTC deliberado: suposição vazada sobre o fuso do processo aparece logo.
		// fsync=off: o banco é descartável.
		testcontainers.WithCmd("postgres", "-c", "timezone=UTC", "-c", "log_timezone=UTC", "-c", "fsync=off"),
		// Espera o log de "pronto" duas vezes (o Postgres reinicia depois do
		// initdb) e a porta. Não substituir por só a porta: ela abre antes de o
		// servidor aceitar conexões ("the database system is starting up").
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, fmt.Errorf("subir %s: %w", image, err)
	}

	connStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("DSN do contêiner: %w", err)
	}
	if err := migrate(ctx, connStr); err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("pool do banco de teste: %w", err)
	}
	return &shared{pool: pool, connStr: connStr}, nil
}

// migrate aplica as migrações do repositório pelo caminho de produção.
func migrate(ctx context.Context, dsn string) error {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := migrations.Up(ctx, dsn, log); err != nil {
		return fmt.Errorf("aplicar migrações no banco de teste: %w", err)
	}
	return nil
}

// Tx devolve uma transação sobre o banco migrado compartilhado; o rollback
// acontece no t.Cleanup. O que o teste fizer aqui não vaza para os outros.
func Tx(t testing.TB) pgx.Tx {
	t.Helper()
	s := get(t)

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("testdb: iniciar transação: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
			t.Errorf("testdb: rollback: %v", err)
		}
	})
	return tx
}

// Pool devolve um pool sobre um banco novo, já migrado, no mesmo contêiner.
// Serve a quem precisa de commit real e visibilidade entre conexões
// (concorrência, "responde só depois do commit", Down de migração). O banco
// é descartado no t.Cleanup.
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	s := get(t)

	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()

	name := "t_" + randomHex(t)
	// O nome é gerado aqui (hex), então interpolá-lo como identificador é seguro.
	if _, err := s.pool.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatalf("testdb: criar banco %s: %v", name, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		if _, err := s.pool.Exec(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
			t.Errorf("testdb: descartar banco %s: %v", name, err)
		}
	})

	// pool.Config().ConnString() devolve este mesmo DSN a quem precisar dele.
	u, err := url.Parse(s.connStr)
	if err != nil {
		t.Fatalf("testdb: DSN do contêiner: %v", err)
	}
	u.Path = "/" + name
	dsn := u.String()
	if err := migrate(ctx, dsn); err != nil {
		t.Fatalf("testdb: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("testdb: abrir pool em %s: %v", name, err)
	}
	// Registrado depois do DROP: roda antes dele (LIFO), fechando as conexões.
	t.Cleanup(pool.Close)
	return pool
}

func randomHex(t testing.TB) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("testdb: aleatório: %v", err)
	}
	return hex.EncodeToString(b)
}
