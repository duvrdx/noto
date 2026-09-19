package migrations_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/duvrdx/noto/internal/platform/migrations"
)

const sentinel = "s3cr3t-dbpass"

// closedPort devolve uma porta de loopback em que ninguém escuta, para a
// conexão ser recusada na hora (uma porta fixa como 1 pode, conforme o
// ambiente, descartar pacotes e só falhar por timeout).
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

func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// Banco inalcançável (porta em que ninguém escuta) devolve erro claro de
// conexão, sem panic e sem vazar a senha do DSN. O teste contra um banco
// real é a task 6.4, com o Postgres do Compose.
func TestUpUnreachableDatabaseReturnsClearError(t *testing.T) {
	dsn := "postgres://noto:" + sentinel + "@" + closedPort(t) + "/noto?sslmode=disable"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := migrations.Up(ctx, dsn, discard())

	if err == nil {
		t.Fatal("Up contra banco inalcançável deveria devolver erro")
	}
	msg := err.Error()
	if !strings.Contains(msg, "conect") {
		t.Errorf("erro deveria dizer que a conexão falhou: %q", msg)
	}
	if strings.Contains(msg, sentinel) {
		t.Errorf("erro vaza a senha do DSN: %q", msg)
	}
}

// DSN malformado também não pode causar panic nem vazar a senha.
func TestUpMalformedDSNDoesNotPanicNorLeakPassword(t *testing.T) {
	for name, dsn := range map[string]string{
		"porta inválida":   "postgres://noto:" + sentinel + "@127.0.0.1:99999/noto",
		"escape inválido":  "postgres://noto:" + sentinel + "%zz@127.0.0.1:5432/noto",
		"vazio":            "",
		"chave=valor ruim": "host=127.0.0.1 port=abc password=" + sentinel,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err := migrations.Up(ctx, dsn, discard())

			if err == nil {
				t.Fatal("Up com DSN inválido deveria devolver erro")
			}
			if strings.Contains(err.Error(), sentinel) {
				t.Errorf("erro vaza a senha do DSN: %q", err.Error())
			}
		})
	}
}
