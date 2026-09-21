package telegram_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"

	"github.com/duvrdx/noto/internal/adapters/telegram"
	"github.com/duvrdx/noto/internal/core/message"
	"github.com/duvrdx/noto/internal/testutil/tgfake"
)

// probeToken tem a forma real (<id>:<segredo>); o segredo isolado também não
// pode aparecer.
const (
	probeToken  = "999999:PROBE-SECRET-TOKEN-abcdef"
	probeSecret = "PROBE-SECRET-TOKEN-abcdef"
)

func assertNoToken(t *testing.T, what, s string) {
	t.Helper()
	for _, secret := range []string{testToken, "SENTINELA-tg-token", probeToken, probeSecret} {
		if strings.Contains(s, secret) {
			t.Errorf("%s contém o token (%q):\n%s", what, secret, s)
		}
	}
}

// entryWithClass devolve as linhas de log com a classificação dada.
func entriesWithClass(t *testing.T, logs *syncBuffer, class string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, e := range logs.entries(t) {
		if e["classe"] == class {
			out = append(out, e)
		}
	}
	return out
}

// Regressão: a biblioteca só redige o token no erro do Do; o de NewRequest
// (URL malformada) o carregava. Nenhuma URL base ruim pode vazar o token.
func TestNewClientBadBaseURLNeverLeaksTheToken(t *testing.T) {
	closed := closedURL(t)
	for name, base := range map[string]string{
		"porta fechada":     closed,
		"host inexistente":  "http://host-que-nao-existe.invalid",
		"esquema ftp":       "ftp://127.0.0.1:1",
		"URL malformada":    "http://[::1",
		"URL com caractere": "http://exemplo.com/\x7f",
	} {
		t.Run(name, func(t *testing.T) {
			captureGlobalLog(t)
			logs := &syncBuffer{}

			_, err := telegram.NewClient(telegram.Config{
				Token: probeToken, APIBaseURL: base,
				Log: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
			})

			if err == nil {
				t.Fatal("NewClient com URL base ruim deveria falhar")
			}
			assertNoToken(t, "o erro de NewClient", err.Error())
			assertNoToken(t, "o log", logs.String())
		})
	}
}

// A redação preserva a cadeia de erros: token rejeitado continua sendo
// bot.ErrorUnauthorized para quem usa errors.Is.
func TestNewClientRejectedTokenKeepsTheErrorChain(t *testing.T) {
	captureGlobalLog(t)
	fake := tgfake.New(t, testToken)
	fake.FailNext("getMe", tgfake.APIError(401, "Unauthorized"))

	_, err := newClientFor(t, fake, &syncBuffer{})

	if !errors.Is(err, bot.ErrorUnauthorized) {
		t.Errorf("errors.Is(err, bot.ErrorUnauthorized) = false: %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "token rejeitado") {
		t.Errorf("o erro deveria dizer \"token rejeitado\": %q", err)
	}
}

// Falha de rede na construção, no envio e numa rodada de polling: o token não
// aparece em nenhum erro devolvido nem em nenhuma linha de log, e a falha do
// polling é registrada (não silenciosa).
func TestNetworkFailureNeverLeaksTheTokenAndPollingFailureIsLogged(t *testing.T) {
	h := start(t, &recorder{})
	tgfake.Eventually(t, wait, func() bool { return h.fake.GetUpdatesCalls() >= 1 }, "poller não iniciou")
	h.fake.Close() // dali em diante, todas as conexões são recusadas

	sendErr := h.client.Notifier().Notify(context.Background(), 7, "oi")

	if sendErr == nil {
		t.Fatal("envio sem rede deveria falhar")
	}
	assertNoToken(t, "o erro do envio", sendErr.Error())
	tgfake.Eventually(t, wait, func() bool { return len(entriesWithClass(t, h.logs, "rede")) >= 1 }, "a falha de rede do polling não foi registrada")
	if err := h.stop(t); err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	assertNoToken(t, "o log", h.logs.String())
}

// Update que não decodifica e carrega texto do usuário: o marcador não vaza
// (a biblioteca o embute no erro), mas o erro de decodificação é registrado.
func TestMalformedUpdateNeverLeaksTheTextButIsLogged(t *testing.T) {
	const marker = "marcador-decode-unico"
	rec := &recorder{}
	h := start(t, rec)

	h.fake.Enqueue(
		// date deveria ser número: o JSON é válido, mas o update não decodifica.
		tgfake.RawUpdate(`{"update_id":90,"message":{"message_id":1,"date":"x","chat":{"id":7,"type":"private"},"text":"`+marker+`"}}`),
		tgfake.TextUpdate(91, 7, 42, "válido"),
	)

	h.waitDone(t, 1)
	tgfake.Eventually(t, wait, func() bool { return len(entriesWithClass(t, h.logs, "decodificacao")) >= 1 },
		"o erro de decodificação não foi registrado:\n"+h.logs.String())
	if strings.Contains(h.logs.String(), marker) {
		t.Errorf("o log contém o texto do update malformado:\n%s", h.logs.String())
	}
	if got := entriesWithClass(t, h.logs, "decodificacao")[0]["update_id"]; got != float64(90) {
		t.Errorf("update_id do erro de decodificação = %v, want 90", got)
	}
	if ids := rec.ids(); len(ids) != 1 || ids[0] != 91 {
		t.Errorf("o poller deveria seguir e entregar o 91: %v", ids)
	}
	// (o pacote log global é conferido, vazio, no Cleanup do harness)
}

// Operação normal, do recebimento ao eco: nem o slog nem o log global veem o texto.
func TestNormalOperationEndToEndNeverLogsTheText(t *testing.T) {
	const marker = "marcador-operacao-unico"
	var h *harness
	rec := &recorder{behave: func(ctx context.Context, in message.Incoming) error {
		return h.client.Notifier().Notify(ctx, in.ChatID, in.Text)
	}}
	h = start(t, rec)

	h.fake.Enqueue(tgfake.TextUpdate(80, 7, 42, marker))

	h.fake.WaitSent(t, 1, wait)
	if got := h.fake.Sent()[0].Text; got != marker {
		t.Errorf("eco = %q, want %q", got, marker)
	}
	h.waitDone(t, 1)
	if strings.Contains(h.logs.String(), marker) {
		t.Errorf("o log contém o texto:\n%s", h.logs.String())
	}
	assertNoToken(t, "o log", h.logs.String())
}

// Um erro do handler que embute o texto do usuário não chega ao log.
func TestHandlerErrorEmbeddingTheTextNeverReachesTheLog(t *testing.T) {
	const marker = "marcador-erro-embutido-unico"
	rec := &recorder{behave: func(_ context.Context, in message.Incoming) error {
		return fmt.Errorf("falha ao processar %q", in.Text)
	}}
	h := start(t, rec)

	h.fake.Enqueue(tgfake.TextUpdate(95, 7, 42, marker))

	h.waitDone(t, 1)
	tgfake.Eventually(t, wait, func() bool { return len(entriesWithClass(t, h.logs, "handler")) >= 1 }, "falha do handler não registrada")
	if strings.Contains(h.logs.String(), marker) {
		t.Errorf("o log contém o texto embutido no erro:\n%s", h.logs.String())
	}
}

// 409: diagnosticável (webhook ativo ou outra instância), uma linha por
// mudança de classe (não uma por tentativa), espera crescente e recuperação
// registrada; o poller não encerra.
func TestConflictIsDiagnosableLoggedOnceBacksOffAndRecovers(t *testing.T) {
	rec := &recorder{}
	h := start(t, rec)
	for range 3 {
		h.fake.FailNext("getUpdates", tgfake.APIError(409, "Conflict: terminated by other getUpdates request"))
	}

	h.fake.Enqueue(tgfake.TextUpdate(60, 7, 42, "depois do conflito"))

	h.waitDone(t, 1)
	tgfake.Eventually(t, wait, func() bool {
		for _, e := range h.logs.entries(t) {
			if e["msg"] == "recuperado" {
				return true
			}
		}
		return false
	}, "faltou o log de recuperação:\n"+h.logs.String())

	conflicts := entriesWithClass(t, h.logs, "conflito")
	if len(conflicts) != 1 {
		t.Fatalf("3 conflitos seguidos deveriam gerar 1 linha, geraram %d:\n%s", len(conflicts), h.logs.String())
	}
	msg, _ := conflicts[0]["msg"].(string)
	for _, want := range []string{"webhook", "outra instância"} {
		if !strings.Contains(msg, want) {
			t.Errorf("a mensagem do conflito deveria mencionar %q: %q", want, msg)
		}
	}
	if conflicts[0]["level"] != "ERROR" {
		t.Errorf("nível do conflito = %v, want ERROR", conflicts[0]["level"])
	}

	// Espera crescente (a da biblioteca: 100 ms, 200 ms, 400 ms). Só cotas
	// inferiores: um temporizador nunca dispara antes do prazo.
	times := h.fake.GetUpdatesTimes()
	if len(times) < 4 {
		t.Fatalf("esperava pelo menos 4 consultas, vieram %d", len(times))
	}
	gaps := []time.Duration{times[1].Sub(times[0]), times[2].Sub(times[1]), times[3].Sub(times[2])}
	for i, min := range []time.Duration{90 * time.Millisecond, 190 * time.Millisecond, 380 * time.Millisecond} {
		if gaps[i] < min {
			t.Errorf("espera %d = %v, want >= %v (espera crescente); todas: %v", i+1, gaps[i], min, gaps)
		}
	}
	select {
	case err := <-h.done:
		t.Fatalf("o poller encerrou (%v) por causa do 409", err)
	default:
	}
}
