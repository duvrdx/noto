package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registra o driver "pgx" do database/sql
	"github.com/pressly/goose/v3"

	sqlmigrations "github.com/duvrdx/noto/migrations"
)

// pingTimeout limita o teste de conexão, para um host que não responde não
// deixar o comando pendurado.
const pingTimeout = 10 * time.Second

// Up aplica as migrações pendentes contra o banco em dsn, usando o goose como
// biblioteca sobre as migrações embutidas. Reexecutar num banco já migrado é
// inerte. Os erros não incluem o DSN: pgx e goose não devolvem a senha, e
// este pacote também não a repete.
func Up(ctx context.Context, dsn string, log *slog.Logger) error {
	// sql.Open com o driver pgx interpreta o DSN já aqui (não só ao conectar),
	// então DSN malformado falha nesta linha.
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("DATABASE_URL inválida: %w", err)
	}
	defer db.Close()

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("conectar ao banco: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, sqlmigrations.FS)
	if err != nil {
		return fmt.Errorf("preparar migrações: %w", err)
	}
	results, err := provider.Up(ctx)
	for _, r := range results {
		log.Info("migração aplicada", "migracao", r.Source.Path, "duracao", r.Duration.String())
	}
	if err != nil {
		return fmt.Errorf("aplicar migrações: %w", err)
	}
	if len(results) == 0 {
		log.Info("nenhuma migração pendente")
	}
	return nil
}
