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

// Spec database-migrations, cenário "A migração inicial continua sem tabelas":
// a 00001 só habilita a extensão. As tabelas do M1 vêm na 00002, e as demais
// com os marcos que as usam; por isso só a 00001 é conferida aqui.
func TestInitialMigrationCreatesNoTables(t *testing.T) {
	b, err := fs.ReadFile(migrations.FS, "00001_enable_pg_trgm.sql")
	if err != nil {
		t.Fatalf("migração inicial não está embutida: %v", err)
	}
	if strings.Contains(strings.ToUpper(string(b)), "CREATE TABLE") {
		t.Error("00001_enable_pg_trgm.sql cria tabela; a migração inicial não pode criar nenhuma")
	}
}
