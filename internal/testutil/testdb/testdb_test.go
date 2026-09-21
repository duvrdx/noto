package testdb_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/duvrdx/noto/internal/testutil/testdb"
)

// childModeEnv liga o modo "processo filho": TestChildProbe só faz algo
// quando o teste pai o relança com esta variável. Os cenários de -short e de
// Docker ausente precisam de outro processo porque dependem de ambiente
// (DOCKER_HOST) e de flag (-test.short) que não se trocam dentro de um
// binário de teste já rodando, e nunca devem tocar o daemon do usuário.
const childModeEnv = "NOTO_TESTDB_CHILD"

func TestTxHasMigrationsAndExtension(t *testing.T) {
	tx := testdb.Tx(t)
	ctx := context.Background()

	var trgm bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm')`).Scan(&trgm); err != nil {
		t.Fatal(err)
	}
	if !trgm {
		t.Error("extensão pg_trgm ausente: a migração 00001 não foi aplicada")
	}

	var versions bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('goose_db_version') IS NOT NULL`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if !versions {
		t.Error("tabela goose_db_version ausente: as migrações não passaram por platform/migrations.Up")
	}

	// Migrações do M1 (00002): as tabelas de domínio existem.
	for _, table := range []string{"users", "messages"} {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("tabela %s ausente: a migração 00002 não foi aplicada", table)
		}
	}
}

func TestServerRunsInUTC(t *testing.T) {
	tx := testdb.Tx(t)

	var tz string
	if err := tx.QueryRow(context.Background(), `SHOW timezone`).Scan(&tz); err != nil {
		t.Fatal(err)
	}
	if tz != "UTC" {
		t.Errorf("timezone = %q, want UTC", tz)
	}
}

// Isolamento por rollback: dois testes em sequência; o segundo não enxerga o
// que o primeiro criou. (Sem users ainda, uma tabela de prova faz o papel.)
func TestTxIsolationFirstWrites(t *testing.T) {
	tx := testdb.Tx(t)
	ctx := context.Background()

	if _, err := tx.Exec(ctx, `CREATE TABLE isolation_probe (n int)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO isolation_probe VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM isolation_probe`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("o próprio teste deveria enxergar a linha: n=%d err=%v", n, err)
	}
}

func TestTxIsolationSecondSeesNothing(t *testing.T) {
	tx := testdb.Tx(t)

	var exists bool
	if err := tx.QueryRow(context.Background(), `SELECT to_regclass('isolation_probe') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("o segundo teste enxerga a tabela criada pelo primeiro: faltou rollback")
	}
}

func TestPoolIsFreshMigratedDatabaseDroppedOnCleanup(t *testing.T) {
	ctx := context.Background()
	var dbName string

	t.Run("uso", func(t *testing.T) {
		pool := testdb.Pool(t)
		if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&dbName); err != nil {
			t.Fatal(err)
		}
		// Commit real, visível a outra conexão do mesmo pool.
		if _, err := pool.Exec(ctx, `CREATE TABLE pool_probe (n int)`); err != nil {
			t.Fatal(err)
		}
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT to_regclass('pool_probe') IS NOT NULL`).Scan(&exists); err != nil || !exists {
			t.Errorf("DDL não ficou visível entre conexões: exists=%v err=%v", exists, err)
		}
		var trgm bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm')`).Scan(&trgm); err != nil || !trgm {
			t.Errorf("banco novo sem migrações: pg_trgm=%v err=%v", trgm, err)
		}
	})

	// O subteste terminou: o Cleanup dele já derrubou o banco.
	tx := testdb.Tx(t)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, dbName).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Errorf("banco %q não foi descartado no Cleanup", dbName)
	}
}

func TestPoolsAreIndependentDatabases(t *testing.T) {
	ctx := context.Background()
	a, b := testdb.Pool(t), testdb.Pool(t)

	if _, err := a.Exec(ctx, `CREATE TABLE only_in_a (n int)`); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := b.QueryRow(ctx, `SELECT to_regclass('only_in_a') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("dois Pool compartilham o mesmo banco")
	}
}

// TestChildProbe é o corpo executado pelo processo filho; fora dele, pula.
func TestChildProbe(t *testing.T) {
	if os.Getenv(childModeEnv) == "" {
		t.Skip("só roda como processo filho de TestShort* e TestNoDocker*")
	}
	testdb.Tx(t)
}

// runChild relança este binário de teste rodando só TestChildProbe, com o
// ambiente ajustado, e devolve a saída combinada e o erro de saída. Com
// noDocker, o filho roda num mount namespace próprio em que /run (onde vive
// /var/run/docker.sock) é um tmpfs vazio, e com DOCKER_HOST e HOME
// apontando para o nada: o testcontainers tenta todos os caminhos e não
// acha daemon. Só o filho vê isso; o daemon e o socket reais não são tocados.
func runChild(t *testing.T, short, noDocker bool) (string, error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	shortFlag := "-test.short=false"
	if short {
		shortFlag = "-test.short=true"
	}
	args := []string{"-test.run=^TestChildProbe$", "-test.v", shortFlag}
	env := append(os.Environ(), childModeEnv+"=1")
	name := exe
	if noDocker {
		// DOCKER_HOST sozinho não basta: o testcontainers cai de volta para
		// /var/run/docker.sock quando o host do ambiente não responde.
		name, args = "unshare", append([]string{"-rm", "sh", "-c", `mount -t tmpfs tmpfs /run && exec "$@"`, "sh", exe}, args...)
		env = append(env, "DOCKER_HOST=unix:///nonexistent/noto-testdb.sock", "HOME="+t.TempDir())
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	return out.String(), err
}

// requireIsolatedNamespaces pula o teste quando o ambiente não deixa criar
// o namespace (não é Linux, unshare ausente ou usuário sem permissão).
func requireIsolatedNamespaces(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("simulação de ausência do Docker usa mount namespace (Linux)")
	}
	out, err := exec.Command("unshare", "-rm", "sh", "-c", "mount -t tmpfs tmpfs /run").CombinedOutput()
	if err != nil {
		t.Skipf("sem permissão para criar mount namespace (%v): %s", err, out)
	}
}

func TestShortSkipsInsteadOfStartingDocker(t *testing.T) {
	out, err := runChild(t, true, false)
	if err != nil {
		t.Fatalf("filho com -short deveria sair com 0: %v\n%s", err, out)
	}
	if !strings.Contains(out, "--- SKIP: TestChildProbe") {
		t.Errorf("filho com -short deveria pular o teste:\n%s", out)
	}
	if strings.Contains(out, "Creating container") {
		t.Errorf("com -short nenhum contêiner deveria ser criado:\n%s", out)
	}
}

// Sem Docker o teste falha alto, citando o Docker.
func TestNoDockerFailsLoudlyNamingDocker(t *testing.T) {
	requireIsolatedNamespaces(t)

	out, err := runChild(t, false, true)
	if err == nil {
		t.Fatalf("filho sem Docker deveria falhar, mas passou:\n%s", out)
	}
	if !strings.Contains(out, "--- FAIL: TestChildProbe") {
		t.Errorf("esperava falha do teste do filho (e não do unshare), veio:\n%s", out)
	}
	if !strings.Contains(out, "Docker é necessário") {
		t.Errorf("a falha deveria dizer que o Docker é necessário:\n%s", out)
	}
	if strings.Contains(out, "--- SKIP") {
		t.Errorf("sem Docker o teste não pode ser pulado em silêncio:\n%s", out)
	}
}
