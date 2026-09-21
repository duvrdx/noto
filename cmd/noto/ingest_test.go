package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/duvrdx/noto/internal/adapters/telegram"
	"github.com/duvrdx/noto/internal/platform/config"
	"github.com/duvrdx/noto/internal/testutil/testdb"
	"github.com/duvrdx/noto/internal/testutil/tgfake"
)

// Token sentinela: nunca o real. Nenhum teste fala com a API real do Telegram.
const (
	e2eToken  = "123456:SENTINELA-e2e-token"
	e2eSecret = "SENTINELA-e2e-token"
	// marcador único: se aparecer em log, o texto do usuário vazou.
	e2eMarker = "marcador-e2e-7f3a-unico"
	wait      = 10 * time.Second
)

// (syncBuffer vem de worker_test.go; aqui só os métodos de log.)
func (b *syncBuffer) logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func (b *syncBuffer) entries(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("linha de log inválida %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// e2e monta a fiação real (runIngest) sobre Postgres real e o Telegram falso.
type e2e struct {
	pool   *pgxpool.Pool
	fake   *tgfake.Server
	logs   *syncBuffer
	addr   string // do servidor HTTP
	cancel context.CancelFunc
	done   chan error
}

func startE2E(t *testing.T) *e2e {
	t.Helper()
	pool := testdb.Pool(t)
	fake := tgfake.New(t, e2eToken)
	logs := &syncBuffer{}
	client, err := telegram.NewClient(telegram.Config{Token: e2eToken, APIBaseURL: fake.URL, Log: logs.logger()})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &e2e{pool: pool, fake: fake, logs: logs, addr: ln.Addr().String(), cancel: cancel, done: make(chan error, 1)}
	go func() { e.done <- runIngest(ctx, ln, client, pool, logs.logger()) }()
	t.Cleanup(func() { e.stop(t) })
	return e
}

// stop cancela e espera a fiação terminar; idempotente.
func (e *e2e) stop(t *testing.T) error {
	t.Helper()
	e.cancel()
	select {
	case err := <-e.done:
		e.done <- err
		return err
	case <-time.After(wait):
		t.Fatal("runIngest não retornou depois do cancelamento")
		return nil
	}
}

func (e *e2e) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Ponta a ponta: update de texto vira uma linha e um eco; a reentrega não
// grava nem responde de novo; o que não é texto privado é descartado; o log
// não tem o texto nem o token; o healthz responde durante o polling.
func TestIngestEndToEnd(t *testing.T) {
	e := startE2E(t)

	// (a) update novo: uma linha e um eco idêntico, no chat certo.
	e.fake.Enqueue(tgfake.TextUpdate(500, 777, 42, e2eMarker))
	e.fake.WaitSent(t, 1, wait)
	sent := e.fake.Sent()
	if len(sent) != 1 || sent[0].ChatID != 777 || sent[0].Text != e2eMarker || sent[0].HasParseMode {
		t.Fatalf("eco = %+v, want um sendMessage (777, %q) sem parse_mode", sent, e2eMarker)
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE telegram_update_id = 500 AND chat_id = 777 AND raw_text = $1`, e2eMarker); n != 1 {
		t.Errorf("messages para o update 500 = %d, want 1", n)
	}

	// (b) reentrega do 500 e, em seguida, o 501: a ordem serial garante que o
	// 500 duplicado já foi processado quando o eco do 501 chega.
	e.fake.Enqueue(tgfake.TextUpdate(500, 777, 42, e2eMarker), tgfake.TextUpdate(501, 777, 42, e2eMarker+"-2"))
	e.fake.WaitSent(t, 2, wait)
	if n := len(e.fake.Sent()); n != 2 {
		t.Errorf("sendMessage = %d, want 2 (o duplicado não responde de novo)", n)
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE telegram_update_id = 500`); n != 1 {
		t.Errorf("messages para o update 500 depois da reentrega = %d, want 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM users`); n != 1 {
		t.Errorf("users = %d, want 1", n)
	}

	// A reentrega é reconhecida como duplicada (log próprio, com o update_id) e
	// não é tratada como falha.
	var dupLogged bool
	for _, entry := range e.logs.entries(t) {
		if entry["msg"] == "mensagem duplicada ignorada" && entry["update_id"] == float64(500) {
			dupLogged = true
		}
		if entry["level"] == "ERROR" {
			t.Errorf("a reentrega não deveria gerar log de erro: %v", entry)
		}
	}
	if !dupLogged {
		t.Errorf("faltou o log de mensagem duplicada com update_id 500:\n%s", e.logs.String())
	}

	// (f) grupo e foto: descartados, sem linha e sem resposta; o poller segue.
	e.fake.Enqueue(
		tgfake.ChatTextUpdate(502, -100, 42, "group", e2eMarker+"-grupo"),
		tgfake.PhotoUpdate(503, 777, 42),
		tgfake.TextUpdate(504, 777, 42, "depois dos descartes"),
	)
	e.fake.WaitSent(t, 3, wait)
	if got := e.fake.Sent()[2].Text; got != "depois dos descartes" {
		t.Errorf("o terceiro eco = %q", got)
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE telegram_update_id IN (502, 503)`); n != 0 {
		t.Errorf("updates descartados gravaram %d linha(s)", n)
	}
	if n := len(e.fake.Sent()); n != 3 {
		t.Errorf("sendMessage = %d, want 3", n)
	}

	// (e) o healthz responde durante o polling, sem depender do Telegram.
	resp, err := http.Get("http://" + e.addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz = %d, want 200", resp.StatusCode)
	}

	// (c) o log não tem o texto nem o token e traz o update_id.
	out := e.logs.String()
	for _, leak := range []string{e2eMarker, e2eToken, e2eSecret} {
		if strings.Contains(out, leak) {
			t.Errorf("o log contém %q:\n%s", leak, out)
		}
	}
	var seen500 bool
	for _, entry := range e.logs.entries(t) {
		if entry["update_id"] == float64(500) {
			seen500 = true
		}
	}
	if !seen500 {
		t.Errorf("o log deveria conter o update_id 500:\n%s", out)
	}

	// (d) cancelar devolve nil e o pool pode ser fechado.
	if err := e.stop(t); err != nil {
		t.Errorf("runIngest = %v, want nil", err)
	}
	closed := make(chan struct{})
	go func() { e.pool.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(wait):
		t.Error("o pool não fechou: há conexão em uso depois do encerramento")
	}
}

// Encerramento: com um update no meio (preso numa escrita no banco), o serve
// só retorna depois de o update terminar, e o update termina com sucesso
// (linha gravada e eco enviado). O pool só é fechado depois disso: serveIngest
// não retorna antes de o polling e o HTTP terminarem. (Fechar o pool antes não
// derrubaria este update em particular, porque pgxpool.Close espera as
// conexões em uso; o que este teste garante é a espera, que é o que evita
// perder o update que ainda vai precisar do pool.)
func TestServeShutdownWaitsForInFlightUpdate(t *testing.T) {
	pool := testdb.Pool(t)
	dsn := pool.Config().ConnString()
	fake := tgfake.New(t, e2eToken)
	logs := &syncBuffer{}
	ctx := context.Background()

	// Uma conexão à parte segura o lock da tabela: o INSERT do update fica preso.
	locker, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close(ctx)
	tx, err := locker.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE messages IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cfg := config.Config{TelegramBotToken: e2eToken, TelegramTransport: config.TransportPolling, DatabaseURL: dsn}
	done := make(chan error, 1)
	go func() { done <- serveIngest(runCtx, cfg, ln, fake.URL, logs.logger()) }()

	fake.Enqueue(tgfake.TextUpdate(900, 777, 42, "preso no banco"))
	tgfake.Eventually(t, wait, func() bool {
		var n int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database()`).Scan(&n)
		return err == nil && n >= 1
	}, "o update não ficou preso no lock do banco")

	cancel()

	select {
	case err := <-done:
		t.Fatalf("serveIngest retornou (%v) com um update ainda em processamento", err)
	case <-time.After(400 * time.Millisecond):
	}
	if err := tx.Rollback(ctx); err != nil { // solta o lock: o INSERT prossegue
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serveIngest = %v, want nil", err)
		}
	case <-time.After(wait):
		t.Fatal("serveIngest não retornou depois de o update terminar")
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE telegram_update_id = 900`).Scan(&n); err != nil || n != 1 {
		t.Errorf("o update em curso deveria ter gravado a linha (pool fechado cedo demais?): n=%d err=%v", n, err)
	}
	if sent := fake.Sent(); len(sent) != 1 || sent[0].Text != "preso no banco" {
		t.Errorf("o eco do update em curso deveria ter sido enviado: %+v", sent)
	}
	for _, e := range logs.entries(t) {
		if e["level"] == "ERROR" {
			t.Errorf("houve log de erro durante o encerramento: %v", e)
		}
	}
}
