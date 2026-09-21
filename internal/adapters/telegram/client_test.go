package telegram_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/duvrdx/noto/internal/adapters/telegram"
	"github.com/duvrdx/noto/internal/core/message"
	"github.com/duvrdx/noto/internal/testutil/tgfake"
)

// Token sentinela: nunca o real. Todos os testes falam só com o tgfake.
const testToken = "123456:SENTINELA-tg-token"

const wait = 5 * time.Second

// syncBuffer é um buffer seguro para o poller escrever enquanto o teste lê.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// entries decodifica as linhas de log JSON.
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

// captureGlobalLog redireciona o pacote log global e exige, no fim do teste,
// que nada tenha sido escrito nele: o adapter só usa o logger injetado.
func captureGlobalLog(t *testing.T) {
	t.Helper()
	var global bytes.Buffer
	prevOut, prevFlags, prevPrefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(&global)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
		if global.Len() != 0 {
			t.Errorf("o pacote log global recebeu saída do adapter: %q", global.String())
		}
	})
}

// recorder é o handler de teste: registra cada chamada e deixa o teste
// bloquear ou falhar updates específicos.
type recorder struct {
	mu      sync.Mutex
	calls   []message.Incoming
	inside  atomic.Int32 // chamadas em andamento
	maxSeen atomic.Int32 // maior sobreposição observada
	ctxErrs []error      // ctx.Err() de cada chamada, no início
	// behave, se não nulo, roda dentro da chamada (pode bloquear); o erro que
	// devolve é o do handler.
	behave func(ctx context.Context, in message.Incoming) error
}

func (r *recorder) handle(ctx context.Context, in message.Incoming) error {
	n := r.inside.Add(1)
	for {
		m := r.maxSeen.Load()
		if n <= m || r.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	defer r.inside.Add(-1)

	r.mu.Lock()
	r.ctxErrs = append(r.ctxErrs, ctx.Err())
	r.mu.Unlock()

	var err error
	if r.behave != nil {
		err = r.behave(ctx, in)
	}

	r.mu.Lock()
	r.calls = append(r.calls, in)
	r.mu.Unlock()
	return err
}

func (r *recorder) done() []message.Incoming {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

func (r *recorder) ids() []int64 {
	var ids []int64
	for _, c := range r.done() {
		ids = append(ids, c.UpdateID)
	}
	return ids
}

// harness liga o cliente ao tgfake e roda o poller numa goroutine.
type harness struct {
	fake   *tgfake.Server
	client *telegram.Client
	logs   *syncBuffer
	rec    *recorder
	cancel context.CancelFunc
	done   chan error
}

func newClientFor(t *testing.T, fake *tgfake.Server, logs *syncBuffer) (*telegram.Client, error) {
	t.Helper()
	return telegram.NewClient(telegram.Config{
		Token:      testToken,
		APIBaseURL: fake.URL,
		Log:        slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
}

// closedURL devolve a URL de um endereço de loopback em que ninguém escuta.
func closedURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return "http://" + addr
}

func start(t *testing.T, rec *recorder) *harness {
	t.Helper()
	captureGlobalLog(t)
	fake := tgfake.New(t, testToken)
	logs := &syncBuffer{}
	client, err := newClientFor(t, fake, logs)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{fake: fake, client: client, logs: logs, rec: rec, cancel: cancel, done: make(chan error, 1)}
	go func() { h.done <- client.Run(ctx, rec.handle) }()
	t.Cleanup(func() { h.stop(t) })
	return h
}

// stop cancela o poller e espera o Run devolver; idempotente.
func (h *harness) stop(t *testing.T) error {
	t.Helper()
	h.cancel()
	select {
	case err := <-h.done:
		h.done <- err // idempotente: quem chamar de novo lê o mesmo resultado
		return err
	case <-time.After(wait):
		t.Fatal("Run não retornou depois do cancelamento")
		return nil
	}
}

func (h *harness) waitDone(t *testing.T, n int) {
	t.Helper()
	tgfake.Eventually(t, wait, func() bool { return len(h.rec.done()) >= n },
		fmt.Sprintf("handler concluiu menos de %d chamadas (concluídas: %v)", n, h.rec.ids()))
}

func TestUpdateReachesHandlerWithMappedFields(t *testing.T) {
	rec := &recorder{}
	h := start(t, rec)

	h.fake.Enqueue(tgfake.TextUpdate(10, 7, 42, "oi"))

	h.waitDone(t, 1)
	want := message.Incoming{UpdateID: 10, ChatID: 7, TelegramUserID: 42, Text: "oi"}
	if got := rec.done(); len(got) != 1 || got[0] != want {
		t.Errorf("handler recebeu %+v, want [%+v]", got, want)
	}
}

func TestOffsetAdvancesPastTheBatch(t *testing.T) {
	h := start(t, &recorder{})

	h.fake.Enqueue(tgfake.TextUpdate(10, 7, 42, "a"), tgfake.TextUpdate(11, 7, 42, "b"))

	h.fake.WaitOffset(t, 12, wait)
	if got := h.fake.Offsets(); got[0] != 1 {
		t.Errorf("a primeira consulta deveria ter offset 1, veio %d", got[0])
	}
}

func TestOnlyMessageUpdatesAreRequested(t *testing.T) {
	h := start(t, &recorder{})
	tgfake.Eventually(t, wait, func() bool { return h.fake.GetUpdatesCalls() >= 2 }, "poller não consultou getUpdates")

	for i, got := range h.fake.AllowedUpdates() {
		if got != `["message"]` {
			t.Errorf("consulta %d: allowed_updates = %q, want [\"message\"]", i, got)
		}
	}
}

// O adapter não deduplica: a reentrega chega de novo ao handler.
func TestRedeliveredUpdateReachesHandlerAgain(t *testing.T) {
	rec := &recorder{}
	h := start(t, rec)

	h.fake.Enqueue(tgfake.TextUpdate(10, 7, 42, "oi"))
	h.waitDone(t, 1)
	h.fake.Enqueue(tgfake.TextUpdate(10, 7, 42, "oi"))

	h.waitDone(t, 2)
	if ids := rec.ids(); !slices.Equal(ids, []int64{10, 10}) {
		t.Errorf("ids entregues = %v, want [10 10]", ids)
	}
}

func TestIgnoredUpdatesSkipHandlerLogReasonAndAdvanceOffset(t *testing.T) {
	rec := &recorder{}
	h := start(t, rec)

	h.fake.Enqueue(
		tgfake.PhotoUpdate(20, 7, 42),
		tgfake.ChatTextUpdate(21, -100, 42, "group", "marcador-ignorado"),
		tgfake.NoFromUpdate(22, 7, "marcador-ignorado"),
		tgfake.TextUpdate(23, 7, 42, "chegou"),
	)

	h.waitDone(t, 1)
	h.fake.WaitOffset(t, 24, wait)
	if ids := rec.ids(); !slices.Equal(ids, []int64{23}) {
		t.Errorf("handler recebeu %v, want só [23]", ids)
	}
	reasons := map[float64]any{}
	for _, e := range h.logs.entries(t) {
		if id, ok := e["update_id"].(float64); ok && e["motivo"] != nil {
			reasons[id] = e["motivo"]
		}
	}
	want := map[float64]any{
		20: string(telegram.ReasonNoText),
		21: string(telegram.ReasonNotPrivate),
		22: string(telegram.ReasonNoSender),
	}
	for id, reason := range want {
		if reasons[id] != reason {
			t.Errorf("update %v: motivo logado = %v, want %v (logs:\n%s)", id, reasons[id], reason, h.logs.String())
		}
	}
	if strings.Contains(h.logs.String(), "marcador-ignorado") {
		t.Errorf("o log contém o texto de um update ignorado:\n%s", h.logs.String())
	}
}

// Erro do handler é logado por update_id, não para o poller, não reconsulta o
// update e não atrasa o seguinte.
func TestHandlerErrorDoesNotStopRepeatOrDelayNextUpdate(t *testing.T) {
	rec := &recorder{behave: func(_ context.Context, in message.Incoming) error {
		if in.UpdateID == 10 {
			return errors.New("marcador-erro-do-handler")
		}
		return nil
	}}
	h := start(t, rec)

	h.fake.Enqueue(tgfake.TextUpdate(10, 7, 42, "falha"), tgfake.TextUpdate(11, 7, 42, "segue"))

	h.waitDone(t, 2)
	h.fake.WaitOffset(t, 12, wait)
	if ids := rec.ids(); !slices.Equal(ids, []int64{10, 11}) {
		t.Errorf("ids = %v, want [10 11]", ids)
	}
	for _, o := range h.fake.Offsets() {
		if o == 10 || o == 11 {
			t.Errorf("uma consulta voltou a pedir o update 10 ou 11 (offset %d); offsets: %v", o, h.fake.Offsets())
		}
	}
	var logged bool
	for _, e := range h.logs.entries(t) {
		if e["update_id"] == float64(10) && e["level"] == "ERROR" {
			logged = true
		}
	}
	if !logged {
		t.Errorf("faltou o log de erro com update_id 10:\n%s", h.logs.String())
	}
	if strings.Contains(h.logs.String(), "marcador-erro-do-handler") {
		t.Errorf("o log repete o texto do erro do handler:\n%s", h.logs.String())
	}
}

// Backpressure: lote de cinco, handler bloqueado no primeiro.
func TestBackpressureLimitsWhatIsConfirmedWithoutBeingProcessed(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	var once, releaseOnce sync.Once
	rec := &recorder{behave: func(ctx context.Context, in message.Incoming) error {
		if in.UpdateID == 30 {
			once.Do(func() { entered <- struct{}{} })
			<-release
		}
		return nil
	}}
	h := start(t, rec)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	h.fake.Enqueue(
		tgfake.TextUpdate(30, 7, 42, "1"), tgfake.TextUpdate(31, 7, 42, "2"), tgfake.TextUpdate(32, 7, 42, "3"),
		tgfake.TextUpdate(33, 7, 42, "4"), tgfake.TextUpdate(34, 7, 42, "5"),
	)
	select {
	case <-entered:
	case <-time.After(wait):
		t.Fatal("o handler não chegou ao primeiro update")
	}

	// Janela de observação (o poller precisaria de uma consulta nova para
	// confirmar mais; o fake segura cada consulta ~50 ms, então 400 ms cobrem
	// várias tentativas).
	time.Sleep(400 * time.Millisecond)

	// Spec: nenhuma consulta com offset acima do do quarto update (33, logo 34).
	for _, o := range h.fake.Offsets() {
		if o > 34 {
			t.Errorf("consulta com offset %d confirmou além do quarto update com o handler bloqueado; offsets: %v", o, h.fake.Offsets())
		}
	}
	// Comportamento observado da combinação cap 1 + 1 worker + não assíncrono:
	// o poller fica preso entregando o lote e nem consulta de novo.
	if n := h.fake.GetUpdatesCalls(); n != 1 {
		t.Errorf("com o handler bloqueado houve %d consultas a getUpdates, observado esperado: 1; offsets: %v", n, h.fake.Offsets())
	}

	releaseOnce.Do(func() { close(release) })
	h.waitDone(t, 5)
	h.fake.WaitOffset(t, 35, wait)
}

func TestHandlerCallsAreSerialAndInOrder(t *testing.T) {
	rec := &recorder{behave: func(_ context.Context, _ message.Incoming) error {
		time.Sleep(10 * time.Millisecond) // dá chance a uma sobreposição, se existisse
		return nil
	}}
	h := start(t, rec)

	h.fake.Enqueue(tgfake.TextUpdate(40, 7, 42, "a"), tgfake.TextUpdate(41, 7, 42, "b"), tgfake.TextUpdate(42, 7, 42, "c"))

	h.waitDone(t, 3)
	if ids := rec.ids(); !slices.Equal(ids, []int64{40, 41, 42}) {
		t.Errorf("ordem = %v, want [40 41 42]", ids)
	}
	if m := rec.maxSeen.Load(); m != 1 {
		t.Errorf("chamadas se sobrepuseram: máximo em andamento = %d, want 1", m)
	}
}

func TestNewClientRejectedTokenFailsNamingItWithoutLeakingIt(t *testing.T) {
	captureGlobalLog(t)
	fake := tgfake.New(t, testToken)
	fake.FailNext("getMe", tgfake.APIError(401, "Unauthorized"))

	_, err := newClientFor(t, fake, &syncBuffer{})

	if err == nil {
		t.Fatal("token rejeitado deveria falhar a construção")
	}
	if !strings.Contains(err.Error(), "token rejeitado") {
		t.Errorf("o erro deveria dizer \"token rejeitado\": %q", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("o erro contém o token: %q", err)
	}
}

// O getMe da subida é mantido como validação do token: um cliente cujo token
// não é o do servidor falha ao construir, com o mesmo erro.
func TestNewClientWithWrongTokenFails(t *testing.T) {
	captureGlobalLog(t)
	fake := tgfake.New(t, testToken)

	_, err := telegram.NewClient(telegram.Config{Token: "999:outro-token", APIBaseURL: fake.URL, Log: slog.New(slog.DiscardHandler)})

	if err == nil || !strings.Contains(err.Error(), "token rejeitado") {
		t.Fatalf("token errado deveria dar \"token rejeitado\", veio: %v", err)
	}
	if strings.Contains(err.Error(), "999:outro-token") {
		t.Errorf("o erro contém o token: %q", err)
	}
}

// Encerramento: o update em andamento conclui com contexto próprio (não
// cancelado pelo encerramento) e Run devolve nil só depois.
func TestShutdownWaitsForInFlightUpdateWithUncancelledContext(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	ctxErr := make(chan error, 1) // ctx.Err() do handler ao terminar, já depois do cancelamento
	var releaseOnce sync.Once
	var hadDeadline atomic.Bool
	rec := &recorder{behave: func(ctx context.Context, _ message.Incoming) error {
		if dl, ok := ctx.Deadline(); ok && time.Until(dl) > 0 && time.Until(dl) <= 8*time.Second {
			hadDeadline.Store(true)
		}
		entered <- struct{}{}
		<-release
		ctxErr <- ctx.Err()
		return nil
	}}
	h := start(t, rec)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	h.fake.Enqueue(tgfake.TextUpdate(50, 7, 42, "em andamento"))
	select {
	case <-entered:
	case <-time.After(wait):
		t.Fatal("o handler não começou")
	}

	h.cancel()

	select {
	case err := <-h.done:
		t.Fatalf("Run retornou (%v) com um update ainda em processamento", err)
	case <-time.After(300 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-h.done:
		if err != nil {
			t.Errorf("Run devolveu %v, want nil", err)
		}
		h.done <- err
	case <-time.After(wait):
		t.Fatal("Run não retornou depois de o handler terminar")
	}

	select {
	case err := <-ctxErr:
		if err != nil {
			t.Errorf("o contexto do handler foi cancelado pelo encerramento: %v", err)
		}
	default:
		t.Fatal("o handler não terminou")
	}
	if !hadDeadline.Load() {
		t.Error("o contexto do handler deveria ter prazo de até 8 s")
	}
	if got := rec.ids(); !slices.Equal(got, []int64{50}) {
		t.Errorf("o update em andamento deveria concluir: %v", got)
	}
}

func TestRunReturnsNilOnCancellation(t *testing.T) {
	h := start(t, &recorder{})
	tgfake.Eventually(t, wait, func() bool { return h.fake.GetUpdatesCalls() >= 1 }, "poller não iniciou")

	if err := h.stop(t); err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
}

// 409 (webhook ativo ou outra instância) não encerra o poller.
func TestConflictDoesNotStopThePoller(t *testing.T) {
	rec := &recorder{}
	h := start(t, rec)
	h.fake.FailNext("getUpdates", tgfake.APIError(409, "Conflict: terminated by other getUpdates request"))
	h.fake.FailNext("getUpdates", tgfake.APIError(409, "Conflict: terminated by other getUpdates request"))

	h.fake.Enqueue(tgfake.TextUpdate(60, 7, 42, "depois do conflito"))

	h.waitDone(t, 1)
	select {
	case err := <-h.done:
		t.Fatalf("o poller encerrou com %v depois de 409", err)
	default:
	}
}

// Quem recusa texto inválido é Incoming.Validate, no caso de uso; o poller
// registra a falha do handler só com o update_id e segue.
func TestRefusedInvalidTextIsLoggedByUpdateIDAndPollerContinues(t *testing.T) {
	const nul = "marcador-nul\x00fim"
	rec := &recorder{behave: func(_ context.Context, in message.Incoming) error { return in.Validate() }}
	h := start(t, rec)

	h.fake.Enqueue(tgfake.TextUpdate(70, 7, 42, nul), tgfake.TextUpdate(71, 7, 42, "válido"))

	h.waitDone(t, 2)
	var logged bool
	for _, e := range h.logs.entries(t) {
		if e["update_id"] == float64(70) && e["level"] == "ERROR" {
			logged = true
		}
	}
	if !logged {
		t.Errorf("faltou o log da recusa com update_id 70:\n%s", h.logs.String())
	}
	if strings.Contains(h.logs.String(), "marcador-nul") {
		t.Errorf("o log contém o texto recusado:\n%s", h.logs.String())
	}
	if got := rec.ids(); !slices.Equal(got, []int64{70, 71}) {
		t.Errorf("ids = %v, want [70 71]", got)
	}
}

// Operação normal: o texto do usuário não aparece no log do adapter.
func TestNormalOperationNeverLogsTheText(t *testing.T) {
	rec := &recorder{}
	h := start(t, rec)

	h.fake.Enqueue(tgfake.TextUpdate(80, 7, 42, "marcador-operacao-normal"))

	h.waitDone(t, 1)
	if strings.Contains(h.logs.String(), "marcador-operacao-normal") {
		t.Errorf("o log contém o texto:\n%s", h.logs.String())
	}
	if strings.Contains(h.logs.String(), testToken) {
		t.Errorf("o log contém o token:\n%s", h.logs.String())
	}
}
