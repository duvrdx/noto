package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/duvrdx/noto/internal/adapters/postgres"
	"github.com/duvrdx/noto/internal/app"
	"github.com/duvrdx/noto/internal/core/message"
	"github.com/duvrdx/noto/internal/core/ports"
	"github.com/duvrdx/noto/internal/testutil/notifiertest"
	"github.com/duvrdx/noto/internal/testutil/testdb"
)

// marker é um texto único e improvável: se aparecer em log, vazou.
const marker = "marcador-9d41f7c2-unico"

func incoming(updateID int64, text string) message.Incoming {
	return message.Incoming{UpdateID: updateID, TelegramUserID: 42, ChatID: 7, Text: text}
}

// logSink captura o slog injetado; é seguro para uso concorrente porque o
// handler serializa as escritas.
type logSink struct{ buf bytes.Buffer }

func (s *logSink) logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&s.buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func (s *logSink) String() string { return s.buf.String() }

// entries decodifica as linhas de log JSON.
func (s *logSink) entries(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(s.buf.String()), "\n") {
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

// fixture monta o caso de uso sobre o repositório real (Postgres via testdb.Pool).
type fixture struct {
	pool     *pgxpool.Pool
	notifier *notifiertest.Notifier
	logs     *logSink
	uc       *app.IngestMessage
}

func newFixture(t *testing.T, notifier *notifiertest.Notifier) *fixture {
	t.Helper()
	pool := testdb.Pool(t)
	logs := &logSink{}
	return &fixture{
		pool:     pool,
		notifier: notifier,
		logs:     logs,
		uc:       app.NewIngestMessage(postgres.NewMessageStore(pool), notifier, logs.logger()),
	}
}

func (f *fixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// fakeStore é um ports.MessageStore que conta chamadas e devolve o que se
// configurar, para os caminhos que o banco real não produz sob demanda.
type fakeStore struct {
	mu    sync.Mutex
	calls int
	res   ports.SaveResult
	err   error
}

func (s *fakeStore) SaveIncoming(_ context.Context, in message.Incoming) (ports.SaveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.res, s.err
}

func (s *fakeStore) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestHandleNewMessageIsStoredAndEchoed(t *testing.T) {
	f := newFixture(t, notifiertest.New())

	out, err := f.uc.Handle(context.Background(), message.Incoming{UpdateID: 100, TelegramUserID: 42, ChatID: 7, Text: "oi"})

	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out != app.Stored {
		t.Errorf("Outcome = %v, want Stored", out)
	}
	if n := f.count(t, `SELECT count(*) FROM users`); n != 1 {
		t.Errorf("users = %d, want 1", n)
	}
	if n := f.count(t, `SELECT count(*) FROM messages WHERE telegram_update_id = 100 AND chat_id = 7 AND raw_text = 'oi'`); n != 1 {
		t.Errorf("messages com os campos esperados = %d, want 1", n)
	}
	calls := f.notifier.Calls()
	if len(calls) != 1 || calls[0].ChatID != 7 || calls[0].Text != "oi" {
		t.Errorf("Notifier recebeu %v, want uma chamada (7, \"oi\")", calls)
	}
}

func TestHandleKnownUserIsReused(t *testing.T) {
	f := newFixture(t, notifiertest.New())
	ctx := context.Background()

	for _, id := range []int64{1, 2} {
		if _, err := f.uc.Handle(ctx, incoming(id, fmt.Sprintf("mensagem %d", id))); err != nil {
			t.Fatal(err)
		}
	}

	if n := f.count(t, `SELECT count(*) FROM users`); n != 1 {
		t.Errorf("users = %d, want 1", n)
	}
	if n := f.count(t, `SELECT count(DISTINCT user_id) FROM messages`); n != 1 {
		t.Errorf("as duas mensagens apontam para %d usuários, want 1", n)
	}
	if n := len(f.notifier.Calls()); n != 2 {
		t.Errorf("respostas enviadas = %d, want 2", n)
	}
}

// A resposta só sai depois do commit: no instante do envio, uma conexão
// independente (fora de qualquer transação do caso de uso) já enxerga a linha.
func TestHandleRepliesOnlyAfterCommit(t *testing.T) {
	notifier := notifiertest.New()
	f := newFixture(t, notifier)
	ctx := context.Background()

	independent, err := pgx.Connect(ctx, f.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer independent.Close(ctx)

	visible := -1
	notifier.OnNotify(func(ctx context.Context, _ int64, _ string) {
		if err := independent.QueryRow(ctx, `SELECT count(*) FROM messages WHERE telegram_update_id = 100`).Scan(&visible); err != nil {
			t.Errorf("consulta da conexão independente: %v", err)
		}
	})

	if _, err := f.uc.Handle(ctx, incoming(100, "oi")); err != nil {
		t.Fatal(err)
	}

	if visible != 1 {
		t.Errorf("no envio a conexão independente via %d linha(s), want 1: a resposta saiu antes do commit", visible)
	}
}

func TestHandlePreservesTextByteForByte(t *testing.T) {
	long := string([]rune(strings.Repeat("ação-ü ", 4096/7+1))[:4096])
	f := newFixture(t, notifiertest.New())
	texts := []string{"áéíóú ç ã õ", "🚀 🇧🇷 👨‍👩‍👧 ✅", "linha 1\nlinha 2\r\n\t\n", "  espaços  ", long}

	for i, text := range texts {
		if _, err := f.uc.Handle(context.Background(), incoming(int64(i+1), text)); err != nil {
			t.Fatal(err)
		}
	}

	calls := f.notifier.Calls()
	for i, text := range texts {
		var stored string
		if err := f.pool.QueryRow(context.Background(), `SELECT raw_text FROM messages WHERE telegram_update_id = $1`, i+1).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != text {
			t.Errorf("texto %d: raw_text difere do recebido (%d vs %d bytes)", i, len(stored), len(text))
		}
		if calls[i].Text != text {
			t.Errorf("texto %d: eco difere do recebido (%d vs %d bytes)", i, len(calls[i].Text), len(text))
		}
	}
}

func TestHandleSequentialRedeliveryIsDuplicate(t *testing.T) {
	f := newFixture(t, notifiertest.New())
	ctx := context.Background()
	if _, err := f.uc.Handle(ctx, incoming(100, "oi")); err != nil {
		t.Fatal(err)
	}

	out, err := f.uc.Handle(ctx, incoming(100, "oi"))

	if err != nil {
		t.Fatalf("reentrega não pode ser erro: %v", err)
	}
	if out != app.Duplicate {
		t.Errorf("Outcome = %v, want Duplicate", out)
	}
	if n := f.count(t, `SELECT count(*) FROM messages WHERE telegram_update_id = 100`); n != 1 {
		t.Errorf("messages = %d, want 1", n)
	}
	if n := len(f.notifier.Calls()); n != 1 {
		t.Errorf("Notifier chamado %d vezes, want 1", n)
	}
}

func TestHandleConcurrentRedeliveryStoresAndRepliesOnce(t *testing.T) {
	f := newFixture(t, notifiertest.New())
	const n = 8

	outs := make([]app.Outcome, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			outs[i], errs[i] = f.uc.Handle(context.Background(), incoming(500, "concorrente"))
		}()
	}
	close(start)
	wg.Wait()

	stored := 0
	for i := range n {
		if errs[i] != nil {
			t.Errorf("chamada %d: %v", i, errs[i])
		}
		if outs[i] == app.Stored {
			stored++
		}
	}
	if stored != 1 {
		t.Errorf("%d chamadas devolveram Stored, want exatamente 1", stored)
	}
	if got := f.count(t, `SELECT count(*) FROM messages WHERE telegram_update_id = 500`); got != 1 {
		t.Errorf("messages = %d, want 1", got)
	}
	if got := len(f.notifier.Calls()); got != 1 {
		t.Errorf("Notifier chamado %d vezes, want 1", got)
	}
}

func TestHandleRedeliveryAfterSendFailureDoesNotResend(t *testing.T) {
	f := newFixture(t, notifiertest.Failing(errors.New("rede caiu")))
	ctx := context.Background()
	if _, err := f.uc.Handle(ctx, incoming(100, "oi")); err == nil {
		t.Fatal("o envio falhou, Handle deveria devolver erro")
	}

	out, err := f.uc.Handle(ctx, incoming(100, "oi"))

	if err != nil {
		t.Fatalf("reentrega não pode ser erro: %v", err)
	}
	if out != app.Duplicate {
		t.Errorf("Outcome = %v, want Duplicate", out)
	}
	if n := len(f.notifier.Calls()); n != 1 {
		t.Errorf("Notifier chamado %d vezes, want 1 (o eco é at-most-once)", n)
	}
}

func TestHandleInvalidIncomingIsRejectedBeforeAnyEffect(t *testing.T) {
	tests := map[string]message.Incoming{
		"texto vazio":         {UpdateID: 1, TelegramUserID: 42, ChatID: 7, Text: ""},
		"UpdateID zero":       {UpdateID: 0, TelegramUserID: 42, ChatID: 7, Text: marker},
		"TelegramUserID zero": {UpdateID: 1, TelegramUserID: 0, ChatID: 7, Text: marker},
		"ChatID zero":         {UpdateID: 1, TelegramUserID: 42, ChatID: 0, Text: marker},
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{}
			notifier := notifiertest.New()
			logs := &logSink{}
			uc := app.NewIngestMessage(store, notifier, logs.logger())

			out, err := uc.Handle(context.Background(), in)

			if err == nil {
				t.Fatal("entrada inválida deveria devolver erro")
			}
			if out == app.Stored || out == app.Duplicate {
				t.Errorf("Outcome = %v com erro", out)
			}
			if store.Calls() != 0 {
				t.Errorf("SaveIncoming foi chamado %d vez(es); a validação deve vir antes", store.Calls())
			}
			if len(notifier.Calls()) != 0 {
				t.Errorf("Notifier chamado: %v", notifier.Calls())
			}
			if strings.Contains(logs.String(), marker) {
				t.Errorf("log contém o texto: %s", logs)
			}
		})
	}
}

func TestHandleInvalidIncomingWritesNothingToTheDatabase(t *testing.T) {
	f := newFixture(t, notifiertest.New())

	if _, err := f.uc.Handle(context.Background(), incoming(1, "")); err == nil {
		t.Fatal("texto vazio deveria devolver erro")
	}

	if n := f.count(t, `SELECT count(*) FROM users`) + f.count(t, `SELECT count(*) FROM messages`); n != 0 {
		t.Errorf("escreveu %d linha(s) para uma entrada inválida", n)
	}
	if n := len(f.notifier.Calls()); n != 0 {
		t.Errorf("Notifier chamado %d vez(es)", n)
	}
}

func TestHandleDatabaseFailureReturnsErrorAndDoesNotReply(t *testing.T) {
	f := newFixture(t, notifiertest.New())
	f.pool.Close() // conexão indisponível

	_, err := f.uc.Handle(context.Background(), incoming(100, marker))

	if err == nil {
		t.Fatal("banco indisponível deveria devolver erro")
	}
	if n := len(f.notifier.Calls()); n != 0 {
		t.Errorf("Notifier chamado %d vez(es) apesar da falha de persistência", n)
	}
}

func TestHandleSendFailurePreservesMessageAndReturnsSendError(t *testing.T) {
	boom := errors.New("telegram indisponível")
	f := newFixture(t, notifiertest.Failing(boom))

	out, err := f.uc.Handle(context.Background(), incoming(100, "oi"))

	if err == nil {
		t.Fatal("falha de envio deveria devolver erro")
	}
	if !errors.Is(err, boom) {
		t.Errorf("o erro do envio deveria ser alcançável por errors.Is: %v", err)
	}
	if out == app.Duplicate {
		t.Errorf("Outcome = %v; a mensagem era nova", out)
	}
	if n := f.count(t, `SELECT count(*) FROM messages WHERE telegram_update_id = 100`); n != 1 {
		t.Errorf("a mensagem deveria continuar persistida: %d linha(s)", n)
	}
	if n := len(f.notifier.Calls()); n != 1 {
		t.Errorf("Notifier chamado %d vezes, want exatamente 1 (sem retentativa)", n)
	}
}

// sendError e storeError provam errors.As através do embrulho do caso de uso.
type sendError struct{ Code int }

func (e *sendError) Error() string { return fmt.Sprintf("envio recusado (%d)", e.Code) }

type storeError struct{ Code int }

func (e *storeError) Error() string { return fmt.Sprintf("banco recusou (%d)", e.Code) }

func TestHandleErrorsUnwrapThroughTheUseCase(t *testing.T) {
	t.Run("persistência", func(t *testing.T) {
		sentinel := &storeError{Code: 503}
		store := &fakeStore{err: fmt.Errorf("camada de baixo: %w", sentinel)}
		uc := app.NewIngestMessage(store, notifiertest.New(), (&logSink{}).logger())

		_, err := uc.Handle(context.Background(), incoming(1, "oi"))

		var got *storeError
		if !errors.As(err, &got) || got != sentinel {
			t.Errorf("errors.As não alcançou o erro do repositório: %v", err)
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("errors.Is não alcançou o erro do repositório: %v", err)
		}
	})
	t.Run("envio", func(t *testing.T) {
		sentinel := &sendError{Code: 403}
		store := &fakeStore{res: ports.SaveResult{UserID: "u", MessageID: "m", Created: true}}
		uc := app.NewIngestMessage(store, notifiertest.Failing(sentinel), (&logSink{}).logger())

		_, err := uc.Handle(context.Background(), incoming(1, "oi"))

		var got *sendError
		if !errors.As(err, &got) || got.Code != 403 {
			t.Errorf("errors.As não alcançou o erro do envio: %v", err)
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("errors.Is não alcançou o erro do envio: %v", err)
		}
	})
}

// --- Logs: só identificadores, nunca o texto ---------------------------------

// logCase roda um caminho do caso de uso e devolve o log capturado.
type logCase struct {
	name string
	run  func(t *testing.T, logs *logSink)
}

func logCases() []logCase {
	return []logCase{
		{"nova", func(t *testing.T, logs *logSink) {
			f := newFixture(t, notifiertest.New())
			f.uc = app.NewIngestMessage(postgres.NewMessageStore(f.pool), f.notifier, logs.logger())
			if _, err := f.uc.Handle(context.Background(), incoming(100, marker)); err != nil {
				t.Fatal(err)
			}
		}},
		{"duplicada", func(t *testing.T, logs *logSink) {
			f := newFixture(t, notifiertest.New())
			f.uc = app.NewIngestMessage(postgres.NewMessageStore(f.pool), f.notifier, logs.logger())
			for range 2 {
				if _, err := f.uc.Handle(context.Background(), incoming(100, marker)); err != nil {
					t.Fatal(err)
				}
			}
		}},
		{"falha de banco (pool fechado)", func(t *testing.T, logs *logSink) {
			f := newFixture(t, notifiertest.New())
			f.pool.Close()
			uc := app.NewIngestMessage(postgres.NewMessageStore(f.pool), f.notifier, logs.logger())
			if _, err := uc.Handle(context.Background(), incoming(100, marker)); err == nil {
				t.Fatal("deveria falhar")
			}
		}},
		{"falha de banco (erro que embute o texto)", func(t *testing.T, logs *logSink) {
			store := &fakeStore{err: fmt.Errorf("falha ao gravar %q", marker)}
			uc := app.NewIngestMessage(store, notifiertest.New(), logs.logger())
			if _, err := uc.Handle(context.Background(), incoming(100, marker)); err == nil {
				t.Fatal("deveria falhar")
			}
		}},
		{"falha de envio (erro que embute o texto)", func(t *testing.T, logs *logSink) {
			f := newFixture(t, notifiertest.Failing(fmt.Errorf("falha ao enviar %q", marker)))
			f.uc = app.NewIngestMessage(postgres.NewMessageStore(f.pool), f.notifier, logs.logger())
			if _, err := f.uc.Handle(context.Background(), incoming(100, marker)); err == nil {
				t.Fatal("deveria falhar")
			}
		}},
		{"falha de envio (erro comum)", func(t *testing.T, logs *logSink) {
			f := newFixture(t, notifiertest.Failing(errors.New("rede caiu")))
			f.uc = app.NewIngestMessage(postgres.NewMessageStore(f.pool), f.notifier, logs.logger())
			if _, err := f.uc.Handle(context.Background(), incoming(100, marker)); err == nil {
				t.Fatal("deveria falhar")
			}
		}},
		{"entrada inválida", func(t *testing.T, logs *logSink) {
			uc := app.NewIngestMessage(&fakeStore{}, notifiertest.New(), logs.logger())
			if _, err := uc.Handle(context.Background(), incoming(0, marker)); err == nil {
				t.Fatal("deveria falhar")
			}
		}},
	}
}

func TestLogsNeverContainTheMessageText(t *testing.T) {
	for _, c := range logCases() {
		t.Run(c.name, func(t *testing.T) {
			logs := &logSink{}

			c.run(t, logs)

			out := logs.String()
			if strings.Contains(out, marker) {
				t.Errorf("o log contém o texto da mensagem:\n%s", out)
			}
			// A ausência de log também mascararia um vazamento futuro.
			if len(logs.entries(t)) == 0 {
				t.Error("o caminho não gerou nenhum log")
			}
		})
	}
}

func TestLogsIdentifyTheUpdateAndDistinguishNewFromDuplicate(t *testing.T) {
	logs := &logSink{}
	f := newFixture(t, notifiertest.New())
	f.uc = app.NewIngestMessage(postgres.NewMessageStore(f.pool), f.notifier, logs.logger())
	ctx := context.Background()

	if _, err := f.uc.Handle(ctx, incoming(100, "oi")); err != nil {
		t.Fatal(err)
	}
	afterNew := len(logs.entries(t))
	if _, err := f.uc.Handle(ctx, incoming(100, "oi")); err != nil {
		t.Fatal(err)
	}

	entries := logs.entries(t)
	if afterNew == 0 || len(entries) <= afterNew {
		t.Fatalf("esperava logs para os dois casos: %d depois da nova, %d no total", afterNew, len(entries))
	}
	newEntry, dupEntry := entries[0], entries[afterNew]
	for name, e := range map[string]map[string]any{"nova": newEntry, "duplicada": dupEntry} {
		if e["update_id"] != float64(100) {
			t.Errorf("log %q sem update_id 100: %v", name, e)
		}
	}
	if newEntry["msg"] == dupEntry["msg"] {
		t.Errorf("logs de nova e duplicada indistinguíveis: %q", newEntry["msg"])
	}
	if newEntry["message_id"] == nil || newEntry["user_id"] == nil || newEntry["chat_id"] == nil {
		t.Errorf("log da nova deveria trazer message_id, user_id e chat_id: %v", newEntry)
	}
}

// Falhas são logadas por classificação, com identificadores; nunca pelo texto
// do erro de uma porta (que pode embutir o texto do usuário).
func TestFailureLogsCarryClassificationAndIdentifiers(t *testing.T) {
	tests := []struct {
		name   string
		build  func(logs *logSink) *app.IngestMessage
		in     message.Incoming
		classe string
	}{
		{"persistência", func(l *logSink) *app.IngestMessage {
			return app.NewIngestMessage(&fakeStore{err: errors.New("banco fora")}, notifiertest.New(), l.logger())
		}, incoming(100, "oi"), "persistencia"},
		{"envio", func(l *logSink) *app.IngestMessage {
			store := &fakeStore{res: ports.SaveResult{UserID: "u1", MessageID: "m1", Created: true}}
			return app.NewIngestMessage(store, notifiertest.Failing(errors.New("rede")), l.logger())
		}, incoming(100, "oi"), "envio"},
		{"entrada inválida", func(l *logSink) *app.IngestMessage {
			return app.NewIngestMessage(&fakeStore{}, notifiertest.New(), l.logger())
		}, incoming(100, ""), "entrada_invalida"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &logSink{}

			if _, err := tt.build(logs).Handle(context.Background(), tt.in); err == nil {
				t.Fatal("deveria falhar")
			}

			// O log da falha é o único com "classe" (o envio falha depois de
			// a mensagem nova ser registrada, que tem o seu próprio log).
			var failures []map[string]any
			for _, e := range logs.entries(t) {
				if e["classe"] != nil {
					failures = append(failures, e)
				}
			}
			if len(failures) != 1 {
				t.Fatalf("esperava 1 log de falha, veio %d:\n%s", len(failures), logs)
			}
			e := failures[0]
			if e["classe"] != tt.classe {
				t.Errorf("classe = %v, want %q", e["classe"], tt.classe)
			}
			if e["update_id"] != float64(100) {
				t.Errorf("update_id = %v, want 100", e["update_id"])
			}
			for _, raw := range []string{"banco fora", "rede"} {
				if strings.Contains(logs.String(), raw) {
					t.Errorf("o log repete o texto do erro da porta (%q):\n%s", raw, logs)
				}
			}
		})
	}
}

// O caso de uso usa só o logger injetado: nada no pacote global log, e o slog
// padrão (que escreve nele) também fica mudo.
func TestNothingGoesToTheGlobalLogger(t *testing.T) {
	var global bytes.Buffer
	prevOut, prevFlags, prevPrefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(&global)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags); log.SetPrefix(prevPrefix) })

	for _, c := range logCases() {
		c.run(t, &logSink{})
	}

	if global.Len() != 0 {
		t.Errorf("o pacote log global recebeu saída: %q", global.String())
	}
}

func TestNewIngestMessageAcceptsNilLogger(t *testing.T) {
	store := &fakeStore{res: ports.SaveResult{UserID: "u", MessageID: "m", Created: true}}
	uc := app.NewIngestMessage(store, notifiertest.New(), nil)

	if _, err := uc.Handle(context.Background(), incoming(1, "oi")); err != nil {
		t.Fatalf("Handle com logger nulo: %v", err)
	}
}
