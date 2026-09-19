package migrations_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/duvrdx/noto/migrations"
)

func TestFSEmbedsInitialMigration(t *testing.T) {
	b, err := fs.ReadFile(migrations.FS, "00001_enable_pg_trgm.sql")
	if err != nil {
		t.Fatalf("migração inicial não está embutida: %v", err)
	}
	sql := string(b)
	for _, want := range []string{"-- +goose Up", "-- +goose Down", "CREATE EXTENSION IF NOT EXISTS pg_trgm", "DROP EXTENSION IF EXISTS pg_trgm"} {
		if !strings.Contains(sql, want) {
			t.Errorf("migração não contém %q", want)
		}
	}
}

// Spec database-migrations: nenhuma tabela do modelo de domínio (PRD §6.2)
// nasce no scaffolding; elas vêm com o código que as usa.
func TestNoMigrationCreatesTables(t *testing.T) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("nenhuma migração embutida")
	}
	for _, e := range entries {
		b, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToUpper(string(b)), "CREATE TABLE") {
			t.Errorf("%s cria tabela; nenhuma é permitida no scaffolding", e.Name())
		}
	}
}
