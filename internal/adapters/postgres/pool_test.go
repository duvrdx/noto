package postgres_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/duvrdx/noto/internal/adapters/postgres"
	"github.com/duvrdx/noto/internal/testutil/testdb"
)

const dbPassSentinel = "s3cr3t-dbpass"

// closedPort devolve host:porta de loopback em que ninguém escuta, para a
// conexão ser recusada na hora.
func closedPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func TestNewPoolReachableDatabasePings(t *testing.T) {
	dsn := testdb.Pool(t).Config().ConnString()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestNewPoolUnreachableDatabaseFailsFastWithoutLeakingPassword(t *testing.T) {
	dsn := "postgres://noto:" + dbPassSentinel + "@" + closedPort(t) + "/noto?sslmode=disable"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	begin := time.Now()

	pool, err := postgres.NewPool(ctx, dsn)

	if err == nil {
		pool.Close()
		t.Fatal("NewPool contra porta fechada deveria devolver erro")
	}
	if pool != nil {
		t.Error("com erro, o pool devolvido deveria ser nil")
	}
	if took := time.Since(begin); took > 5*time.Second {
		t.Errorf("erro demorou %v; porta recusada deveria falhar na hora", took)
	}
	if !strings.Contains(err.Error(), "conect") {
		t.Errorf("erro deveria dizer que a conexão falhou: %q", err)
	}
	if strings.Contains(err.Error(), dbPassSentinel) {
		t.Errorf("erro vaza a senha do DSN: %q", err)
	}
}

// (Sem o caso de DSN vazio: o pgx o completaria com padrões do ambiente e
// tentaria um Postgres local de verdade.)
func TestNewPoolMalformedDSNDoesNotPanicNorLeakPassword(t *testing.T) {
	for name, dsn := range map[string]string{
		"porta inválida":   "postgres://noto:" + dbPassSentinel + "@127.0.0.1:99999/noto",
		"escape inválido":  "postgres://noto:" + dbPassSentinel + "%zz@127.0.0.1:5432/noto",
		"chave=valor ruim": "host=127.0.0.1 port=abc password=" + dbPassSentinel,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			pool, err := postgres.NewPool(ctx, dsn)

			if err == nil {
				pool.Close()
				t.Fatal("DSN inválido deveria devolver erro")
			}
			if strings.Contains(err.Error(), dbPassSentinel) {
				t.Errorf("erro vaza a senha do DSN: %q", err)
			}
		})
	}
}
