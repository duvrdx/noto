package main

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/duvrdx/noto/internal/platform/config"
)

// closedAddr devolve host:porta de loopback em que ninguém escuta, para a
// conexão ser recusada na hora.
func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// noto migrate contra um banco inalcançável falha com erro claro de conexão
// (código != 0), sem panic e sem a senha do DSN em stderr nem em stdout.
func TestMigrateUnreachableDatabaseFailsClearlyWithoutLeakingPassword(t *testing.T) {
	const pass = "s3cr3t-dbpass"
	t.Setenv("DATABASE_URL", "postgres://noto:"+pass+"@"+closedAddr(t)+"/noto?sslmode=disable")
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	code := defaultCLI(&stdout, &stderr).run(ctx, []string{"migrate"})

	if code == 0 {
		t.Fatal("código = 0, want != 0")
	}
	if !strings.Contains(stderr.String(), "conect") {
		t.Errorf("stderr deveria trazer erro de conexão:\n%s", stderr.String())
	}
	for name, out := range map[string]string{"stderr": stderr.String(), "stdout": stdout.String()} {
		if strings.Contains(out, pass) {
			t.Errorf("%s vaza a senha do DSN:\n%s", name, out)
		}
	}
}

func TestRunMigrateIsNotAStub(t *testing.T) {
	cfg := config.Config{DatabaseURL: "postgres://noto:x@" + closedAddr(t) + "/noto?sslmode=disable"}
	err := runMigrate(context.Background(), cfg, discardLogger())
	if err == nil {
		t.Fatal("banco inalcançável deveria dar erro")
	}
	if strings.Contains(err.Error(), "ainda não implementado") {
		t.Errorf("runMigrate ainda é o stub: %v", err)
	}
}
