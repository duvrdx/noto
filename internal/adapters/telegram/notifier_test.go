package telegram_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"

	"github.com/duvrdx/noto/internal/core/ports"
	"github.com/duvrdx/noto/internal/testutil/tgfake"
)

// notifierFor devolve um Notifier sobre o tgfake, sem rodar o polling.
func notifierFor(t *testing.T) (ports.Notifier, *tgfake.Server, *syncBuffer) {
	t.Helper()
	captureGlobalLog(t)
	fake := tgfake.New(t, testToken)
	logs := &syncBuffer{}
	client, err := newClientFor(t, fake, logs)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client.Notifier(), fake, logs
}

// Texto puro: nenhum caractere do usuário vira formatação, e nenhum parse_mode
// vai na requisição.
func TestNotifyDeliversTextIntactAndWithoutParseMode(t *testing.T) {
	n, fake, _ := notifierFor(t)
	const text = "*negrito* _itálico_ <b>html</b> [x](y) `code`\nlinha 2 ação 🚀"

	if err := n.Notify(context.Background(), 7, text); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	sent := fake.Sent()
	if len(sent) != 1 {
		t.Fatalf("sendMessage recebido %d vezes, want 1", len(sent))
	}
	if sent[0].ChatID != 7 || sent[0].Text != text {
		t.Errorf("recebido (%d, %q), want (7, %q)", sent[0].ChatID, sent[0].Text, text)
	}
	if sent[0].HasParseMode {
		t.Errorf("a requisição trouxe parse_mode %q; deveria não ter o campo", sent[0].ParseMode)
	}
}

// 403: o usuário bloqueou o bot. Erro que identifica o bloqueio, uma tentativa só.
func TestNotifyForbiddenIdentifiesBlockAndDoesNotRetry(t *testing.T) {
	n, fake, _ := notifierFor(t)
	fake.FailNext("sendMessage", tgfake.APIError(403, "Forbidden: bot was blocked by the user"))

	err := n.Notify(context.Background(), 7, "oi")

	if err == nil {
		t.Fatal("403 deveria virar erro")
	}
	if !strings.Contains(err.Error(), "bloqueou o bot") {
		t.Errorf("o erro deveria identificar o bloqueio: %q", err)
	}
	if !errors.Is(err, bot.ErrorForbidden) {
		t.Errorf("errors.Is(err, bot.ErrorForbidden) = false: %v", err)
	}
	assertSingleAttempt(t, fake)
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("o erro contém o token: %q", err)
	}
}

func TestNotifyRateLimitedIsAnErrorWithoutRetry(t *testing.T) {
	n, fake, _ := notifierFor(t)
	fake.FailNext("sendMessage", tgfake.RetryAfter(1))

	err := n.Notify(context.Background(), 7, "oi")

	var tooMany *bot.TooManyRequestsError
	if !errors.As(err, &tooMany) || tooMany.RetryAfter != 1 {
		t.Fatalf("esperava *bot.TooManyRequestsError com RetryAfter 1: %v", err)
	}
	if !strings.Contains(err.Error(), "limite de taxa") {
		t.Errorf("o erro deveria dizer que é limite de taxa: %q", err)
	}
	assertSingleAttempt(t, fake)
}

func TestNotifyBadRequestAndServerErrorDoNotRetry(t *testing.T) {
	for name, reply := range map[string]tgfake.Reply{
		"400": tgfake.APIError(400, "Bad Request: chat not found"),
		"500": tgfake.APIError(500, "Internal Server Error"),
	} {
		t.Run(name, func(t *testing.T) {
			n, fake, _ := notifierFor(t)
			fake.FailNext("sendMessage", reply)

			if err := n.Notify(context.Background(), 7, "oi"); err == nil {
				t.Fatal("deveria devolver erro")
			}

			assertSingleAttempt(t, fake)
		})
	}
}

// assertSingleAttempt exige uma única tentativa, também depois de uma folga
// (uma retentativa em segundo plano chegaria nela).
func assertSingleAttempt(t *testing.T, fake *tgfake.Server) {
	t.Helper()
	time.Sleep(150 * time.Millisecond)
	if got := fake.Attempts("sendMessage"); got != 1 {
		t.Errorf("a API recebeu %d tentativas de sendMessage, want exatamente 1", got)
	}
}

// Erro de rede (conexão derrubada) não carrega o token.
func TestNotifyNetworkErrorDoesNotContainTheToken(t *testing.T) {
	n, fake, _ := notifierFor(t)
	fake.FailNext("sendMessage", tgfake.Hangup())

	err := n.Notify(context.Background(), 7, "oi")

	if err == nil {
		t.Fatal("conexão derrubada deveria virar erro")
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "SENTINELA-tg-token") {
		t.Errorf("o erro contém o token: %q", err)
	}
	assertSingleAttempt(t, fake)
}

// O Notifier usa o contexto recebido: cancelado, nem tenta.
func TestNotifyHonorsTheCallerContext(t *testing.T) {
	n, fake, _ := notifierFor(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := n.Notify(ctx, 7, "oi")

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled na cadeia", err)
	}
	if got := fake.Attempts("sendMessage"); got != 0 {
		t.Errorf("com o contexto cancelado houve %d tentativa(s)", got)
	}
}
