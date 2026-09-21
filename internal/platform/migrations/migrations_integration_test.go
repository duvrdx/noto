package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // driver "pgx" do database/sql, para o goose
	"github.com/pressly/goose/v3"

	sqlmigrations "github.com/duvrdx/noto/migrations"

	"github.com/duvrdx/noto/internal/platform/migrations"
	"github.com/duvrdx/noto/internal/testutil/testdb"
)

// SQLSTATE das violações que os cenários provocam.
const (
	uniqueViolation = "23505"
	fkViolation     = "23503"
)

type column struct {
	name, typ, nullable, def string
}

// columns lê o catálogo: nome, tipo, nulidade e default de cada coluna, em ordem.
func columns(t *testing.T, pool *pgxpool.Pool, table string) []column {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT column_name, data_type, is_nullable, coalesce(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		ORDER BY ordinal_position`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.typ, &c.nullable, &c.def); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('public.' || $1) IS NOT NULL`, name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func extensionExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)`, name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

// sqlState devolve o SQLSTATE do erro do Postgres, ou "" se não for um.
func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestUsersAndMessagesHaveExactlyTheSpecifiedColumns(t *testing.T) {
	pool := testdb.Pool(t)

	want := map[string][]column{
		"users": {
			{"id", "uuid", "NO", "gen_random_uuid()"},
			{"telegram_user_id", "bigint", "NO", ""},
			{"created_at", "timestamp with time zone", "NO", "now()"},
		},
		"messages": {
			{"id", "uuid", "NO", "gen_random_uuid()"},
			{"user_id", "uuid", "NO", ""},
			{"telegram_update_id", "bigint", "NO", ""},
			{"chat_id", "bigint", "NO", ""},
			{"raw_text", "text", "NO", ""},
			{"received_at", "timestamp with time zone", "NO", "now()"},
		},
	}
	for table, cols := range want {
		got := columns(t, pool, table)
		if !slices.Equal(got, cols) {
			t.Errorf("colunas de %s:\n got  %v\n want %v", table, got, cols)
		}
	}
}

func TestPrimaryKeysAndUniqueConstraints(t *testing.T) {
	pool := testdb.Pool(t)

	// (tabela, coluna, tipo de restrição) que precisam existir.
	for _, w := range []struct{ table, column, kind string }{
		{"users", "id", "PRIMARY KEY"},
		{"users", "telegram_user_id", "UNIQUE"},
		{"messages", "id", "PRIMARY KEY"},
		{"messages", "telegram_update_id", "UNIQUE"},
		{"messages", "user_id", "FOREIGN KEY"},
	} {
		var n int
		err := pool.QueryRow(context.Background(), `
			SELECT count(*)
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
			  ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
			WHERE tc.table_schema = 'public' AND tc.table_name = $1
			  AND kcu.column_name = $2 AND tc.constraint_type = $3`, w.table, w.column, w.kind).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s.%s: %d restrições %s, want 1", w.table, w.column, n, w.kind)
		}
	}
}

func TestDuplicateUpdateIDIsRejectedByTheDatabase(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()

	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO users (telegram_user_id) VALUES (1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("inserir usuário: %v", err)
	}
	insert := `INSERT INTO messages (user_id, telegram_update_id, chat_id, raw_text) VALUES ($1, 42, 7, 'oi')`
	if _, err := pool.Exec(ctx, insert, userID); err != nil {
		t.Fatalf("primeira mensagem: %v", err)
	}

	_, err := pool.Exec(ctx, insert, userID)

	if got := sqlState(err); got != uniqueViolation {
		t.Errorf("segundo insert com o mesmo telegram_update_id: SQLSTATE %q (%v), want %s", got, err, uniqueViolation)
	}
}

func TestDuplicateTelegramUserIDIsRejectedByTheDatabase(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `INSERT INTO users (telegram_user_id) VALUES (1)`); err != nil {
		t.Fatalf("primeiro usuário: %v", err)
	}

	_, err := pool.Exec(ctx, `INSERT INTO users (telegram_user_id) VALUES (1)`)

	if got := sqlState(err); got != uniqueViolation {
		t.Errorf("segundo usuário igual: SQLSTATE %q (%v), want %s", got, err, uniqueViolation)
	}
}

func TestMessageRequiresExistingUser(t *testing.T) {
	pool := testdb.Pool(t)

	_, err := pool.Exec(context.Background(), `
		INSERT INTO messages (user_id, telegram_update_id, chat_id, raw_text)
		VALUES (gen_random_uuid(), 1, 7, 'oi')`)

	if got := sqlState(err); got != fkViolation {
		t.Errorf("mensagem de usuário inexistente: SQLSTATE %q (%v), want %s", got, err, fkViolation)
	}
}

func TestColumnsAndTablesOfOtherMilestonesDoNotExist(t *testing.T) {
	pool := testdb.Pool(t)

	for _, absent := range []struct{ table, column string }{
		{"users", "timezone"},
		{"users", "locale"},
		{"messages", "telegram_message_id"},
	} {
		for _, c := range columns(t, pool, absent.table) {
			if c.name == absent.column {
				t.Errorf("%s.%s existe, mas pertence a outro marco", absent.table, absent.column)
			}
		}
	}
	for _, table := range []string{"items", "reminders", "outbox", "pending_questions", "item_revisions", "parse_runs", "resolutions"} {
		if tableExists(t, pool, table) {
			t.Errorf("tabela %s existe, mas pertence a outro marco", table)
		}
	}
	// Sem estas duas o teste passaria no vazio (colunas vazias não têm o que proibir).
	for _, table := range []string{"users", "messages"} {
		if !tableExists(t, pool, table) {
			t.Errorf("tabela %s não existe", table)
		}
	}
}

func TestUpAgainIsInert(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	dsn := pool.Config().ConnString()

	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM goose_db_version`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	if err := migrations.Up(ctx, dsn, discard()); err != nil {
		t.Fatalf("Up reaplicado deveria ser inerte, veio erro: %v", err)
	}

	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM goose_db_version`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("Up reaplicado mudou goose_db_version: %d -> %d linhas", before, after)
	}
}

// Down da 00002 (uma versão para baixo, goose direto sobre a conexão de
// teste) remove messages e users e preserva pg_trgm; Up refaz o esquema.
func TestDownOf00002RemovesTablesAndKeepsExtension(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	if !tableExists(t, pool, "users") || !tableExists(t, pool, "messages") {
		t.Fatal("pré-condição: users e messages deveriam existir antes do Down")
	}

	db, err := sql.Open("pgx", pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, sqlmigrations.FS)
	if err != nil {
		t.Fatal(err)
	}

	res, err := provider.Down(ctx)
	if err != nil {
		t.Fatalf("Down: %v", err)
	}
	if got, want := res.Source.Path, "00002_create_users_and_messages.sql"; got != want {
		t.Fatalf("Down reverteu %q, want %q", got, want)
	}

	for _, table := range []string{"messages", "users"} {
		if tableExists(t, pool, table) {
			t.Errorf("tabela %s continua existindo depois do Down", table)
		}
	}
	if !extensionExists(t, pool, "pg_trgm") {
		t.Error("o Down da 00002 removeu pg_trgm, que é da 00001")
	}

	// O ciclo fecha: reaplicar recria as duas tabelas.
	if err := migrations.Up(ctx, pool.Config().ConnString(), discard()); err != nil {
		t.Fatalf("Up depois do Down: %v", err)
	}
	for _, table := range []string{"users", "messages"} {
		if !tableExists(t, pool, table) {
			t.Errorf("tabela %s não voltou com o Up", table)
		}
	}
}
