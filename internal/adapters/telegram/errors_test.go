package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/go-telegram/bot"
)

const (
	unitToken  = "999999:UNIT-SECRET-TOKEN-abcdef"
	unitSecret = "UNIT-SECRET-TOKEN-abcdef"
)

// --- classify ----------------------------------------------------------------

func TestClassify(t *testing.T) {
	jsonErr := func() error {
		var v struct{ N int }
		return json.Unmarshal([]byte(`{"N":"x"}`), &v)
	}()
	syntaxErr := json.Unmarshal([]byte(`{`), new(any))

	tests := []struct {
		name string
		err  error
		want errClass
	}{
		{"conflito", fmt.Errorf("error get updates, %w", fmt.Errorf("%w, %s", bot.ErrorConflict, "Conflict: x")), classConflict},
		{"não autorizado", fmt.Errorf("x: %w, %s", bot.ErrorUnauthorized, "Unauthorized"), classUnauthorized},
		{"proibido", fmt.Errorf("%w, %s", bot.ErrorForbidden, "blocked"), classForbidden},
		{"requisição inválida", fmt.Errorf("%w, %s", bot.ErrorBadRequest, "chat not found"), classBadRequest},
		{"migração de chat é requisição inválida", &bot.MigrateError{Message: "x", MigrateToChatID: 5}, classBadRequest},
		{"limite de taxa (tipado)", fmt.Errorf("x: %w", &bot.TooManyRequestsError{Message: "m", RetryAfter: 3}), classRateLimit},
		{"limite de taxa (sentinela)", fmt.Errorf("%w", bot.ErrorTooManyRequests), classRateLimit},
		{"cancelado", &url.Error{Op: "Post", URL: "http://x", Err: context.Canceled}, classCanceled},
		{"rede: url.Error", &url.Error{Op: "Post", URL: "http://x", Err: errors.New("dial tcp: refused")}, classNetwork},
		{"rede: net.OpError", &net.OpError{Op: "dial", Err: errors.New("refused")}, classNetwork},
		{"rede: fim inesperado", &url.Error{Op: "Post", URL: "http://x", Err: io.ErrUnexpectedEOF}, classNetwork},
		{"rede: prazo estourado", context.DeadlineExceeded, classNetwork},
		{"decodificação: tipo", fmt.Errorf("error decode update 5, skipped, %s, %w", `{"marcador"}`, jsonErr), classDecode},
		{"decodificação: sintaxe", fmt.Errorf("error decode response body for method getUpdates, %s, %w", "lixo", syntaxErr), classDecode},
		{"decodificação: sem update_id (só o prefixo da biblioteca)", fmt.Errorf("error decode update, %s, %w", "{}", errors.New("missing update_id")), classDecode},
		{"outro", errors.New("algo inesperado"), classOther},
		// Nunca por texto: o texto de um erro qualquer que cita "conflict" não é conflito.
		{"texto que cita conflito não é conflito", errors.New("conflict: unauthorized forbidden"), classOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classify(tt.err); got != tt.want {
				t.Errorf("classify(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestDecodeErrorUpdateID(t *testing.T) {
	tests := []struct {
		err    error
		wantID int64
		wantOK bool
	}{
		{fmt.Errorf("error decode update 90, skipped, %s, %w", `{"text":"marcador"}`, errors.New("boom")), 90, true},
		{fmt.Errorf("error decode update, %s, %w", `{}`, errors.New("missing update_id")), 0, false},
		{fmt.Errorf("error get updates, %w", bot.ErrorConflict), 0, false},
		{errors.New("error decode update 12x, skipped"), 0, false},
	}
	for _, tt := range tests {
		id, ok := decodeErrorUpdateID(tt.err)
		if id != tt.wantID || ok != tt.wantOK {
			t.Errorf("decodeErrorUpdateID(%q) = (%d, %v), want (%d, %v)", tt.err, id, ok, tt.wantID, tt.wantOK)
		}
	}
}

// --- redact ------------------------------------------------------------------

func TestRedactNilAndUntouched(t *testing.T) {
	if got := redact(nil, unitToken); got != nil {
		t.Errorf("redact(nil) = %v", got)
	}
	orig := errors.New("nada secreto aqui")
	if got := redact(orig, unitToken); got != orig {
		t.Errorf("erro sem o token deveria voltar idêntico, veio %v", got)
	}
}

func TestRedactReplacesEveryForm(t *testing.T) {
	esc := url.QueryEscape(unitToken) // 999999%3AUNIT-...
	tests := map[string]string{
		"token inteiro":        "falhou com " + unitToken + " no meio",
		"na URL, com bot":      `Post "https://api.telegram.org/bot` + unitToken + `/getMe": refused`,
		"segredo isolado":      "cortado em :" + unitSecret,
		"percent-encoded":      "url " + esc + " aqui",
		"várias ocorrências":   unitToken + " e " + unitToken + " e " + unitSecret,
		"path escaped":         url.PathEscape(unitToken),
		"segredo entre aspas":  `"` + unitSecret + `"`,
		"no início e no fim":   unitToken,
		"colado em mais texto": "x" + unitSecret + "y",
	}
	for name, msg := range tests {
		t.Run(name, func(t *testing.T) {
			got := redact(errors.New(msg), unitToken)

			for _, leak := range []string{unitToken, unitSecret, esc} {
				if strings.Contains(got.Error(), leak) {
					t.Errorf("continua vazando %q: %q", leak, got)
				}
			}
			if !strings.Contains(got.Error(), "[REDACTED]") {
				t.Errorf("sem marcador de redação: %q", got)
			}
		})
	}
}

func TestRedactKeepsUsefulContext(t *testing.T) {
	got := redact(fmt.Errorf(`Post "http://x/bot%s/getMe": connection refused`, unitToken), unitToken)

	if want := `Post "http://x/bot[REDACTED]/getMe": connection refused`; got.Error() != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A redação embrulha, não achata: errors.Is e errors.As seguem funcionando.
func TestRedactPreservesErrorChain(t *testing.T) {
	urlErr := &url.Error{Op: "Post", URL: "http://x/bot" + unitToken + "/getMe", Err: bot.ErrorUnauthorized}
	err := redact(fmt.Errorf("getMe: %w", urlErr), unitToken)

	if strings.Contains(err.Error(), unitToken) {
		t.Fatalf("vazou: %q", err)
	}
	if !errors.Is(err, bot.ErrorUnauthorized) {
		t.Error("errors.Is(bot.ErrorUnauthorized) perdeu-se na redação")
	}
	var got *url.Error
	if !errors.As(err, &got) || got.Op != "Post" {
		t.Error("errors.As(*url.Error) perdeu-se na redação")
	}
	// Embrulhar de novo por cima do erro redigido continua limpo.
	if outer := fmt.Errorf("enviar: %w", err); strings.Contains(outer.Error(), unitSecret) {
		t.Errorf("o embrulho externo vazou: %q", outer)
	}
}

func TestRedactWithEmptyTokenIsANoOp(t *testing.T) {
	orig := errors.New("qualquer coisa")
	if got := redact(orig, ""); got != orig {
		t.Errorf("token vazio deveria devolver o erro como está, veio %v", got)
	}
}

// Um token sem ":" também é redigido (inteiro).
func TestRedactTokenWithoutColon(t *testing.T) {
	got := redact(errors.New("erro com tokensemdoispontos aqui"), "tokensemdoispontos")

	if strings.Contains(got.Error(), "tokensemdoispontos") {
		t.Errorf("vazou: %q", got)
	}
}

// --- reporter e onLibraryError ------------------------------------------------

type lineSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *lineSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *lineSink) lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s.buf.String()), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func newTestClient() (*Client, *lineSink) {
	sink := &lineSink{}
	log := slog.New(slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &Client{log: log, reporter: newReporter(log)}, sink
}

func conflictErr() error {
	return fmt.Errorf("error get updates, %w", fmt.Errorf("%w, %s", bot.ErrorConflict, "Conflict: terminated by other getUpdates request"))
}

// 1000 erros seguidos da mesma classe não viram 1000 linhas: a primeira e as
// de potência de dez (10, 100, 1000) com o contador.
func TestThousandConsecutiveErrorsOfTheSameClassAreSuppressed(t *testing.T) {
	c, sink := newTestClient()

	for range 1000 {
		c.onLibraryError(conflictErr())
	}

	lines := sink.lines()
	if len(lines) != 4 {
		t.Fatalf("1000 erros geraram %d linhas, want 4 (1, 10, 100, 1000):\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[3], `"repeticoes":1000`) {
		t.Errorf("a última linha deveria trazer o contador 1000: %s", lines[3])
	}
}

func TestClassChangeIsLoggedAndRecoveryResetsTheCounter(t *testing.T) {
	c, sink := newTestClient()

	c.onLibraryError(conflictErr())
	c.onLibraryError(conflictErr()) // suprimido
	c.onLibraryError(fmt.Errorf("error get updates, %w", &url.Error{Op: "Post", URL: "http://x", Err: errors.New("refused")}))
	c.pollSucceeded()
	c.pollSucceeded()               // sem falha pendente: nada
	c.onLibraryError(conflictErr()) // depois da recuperação, volta a ser a primeira

	lines := sink.lines()
	want := []string{`"classe":"conflito"`, `"classe":"rede"`, `"msg":"recuperado"`, `"classe":"conflito"`}
	if len(lines) != len(want) {
		t.Fatalf("%d linhas, want %d:\n%s", len(lines), len(want), strings.Join(lines, "\n"))
	}
	for i, w := range want {
		if !strings.Contains(lines[i], w) {
			t.Errorf("linha %d deveria conter %s: %s", i, w, lines[i])
		}
	}
}

func TestLibraryErrorLogsNeverContainTheRawErrorText(t *testing.T) {
	c, sink := newTestClient()
	raw := `{"update_id":7,"message":{"text":"marcador-bruto-unico"}}`

	c.onLibraryError(fmt.Errorf("error decode update 7, skipped, %s, %w", raw, errors.New("json: cannot unmarshal")))
	c.onLibraryError(errors.New("erro qualquer com marcador-bruto-unico"))
	c.onLibraryError(fmt.Errorf("x: %w", &bot.TooManyRequestsError{Message: "marcador-bruto-unico", RetryAfter: 7}))

	out := strings.Join(sink.lines(), "\n")
	if strings.Contains(out, "marcador-bruto-unico") {
		t.Errorf("o log repete o texto de um erro da biblioteca:\n%s", out)
	}
	for _, want := range []string{`"classe":"decodificacao"`, `"update_id":7`, `"classe":"outro"`, `"classe":"limite_de_taxa"`, `"retry_after_s":7`} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %s no log:\n%s", want, out)
		}
	}
}

// Depois do cancelamento do Run, erros da biblioteca são ruído de encerramento.
func TestErrorsAfterRunContextCancelledAreClassifiedAsCancelled(t *testing.T) {
	c, sink := newTestClient()
	ctx, cancel := context.WithCancel(context.Background())
	c.runCtx = ctx
	cancel()

	c.onLibraryError(errors.New("some updates lost, ctx done"))

	lines := sink.lines()
	if len(lines) != 1 || !strings.Contains(lines[0], `"classe":"contexto_cancelado"`) || strings.Contains(lines[0], `"level":"ERROR"`) {
		t.Errorf("esperava uma linha contexto_cancelado abaixo de ERROR: %v", lines)
	}
}

func TestReporterIsSafeForConcurrentUse(t *testing.T) {
	c, _ := newTestClient()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				c.onLibraryError(conflictErr())
				c.pollSucceeded()
			}
		}()
	}
	wg.Wait()
}

// O Notifier redige o token de todo erro do envio, também dos que a biblioteca
// não redige (por exemplo o de NewRequest), e mantém a cadeia.
func TestNotifyRedactsTheTokenFromEverySendError(t *testing.T) {
	c := &Client{token: unitToken, send: func(context.Context, *bot.SendMessageParams) error {
		return fmt.Errorf("error create request for method sendMessage, parse %q: missing ']' in host: %w",
			"http://[::1/bot"+unitToken+"/sendMessage", bot.ErrorForbidden)
	}}

	err := c.Notifier().Notify(context.Background(), 7, "oi")

	if err == nil {
		t.Fatal("deveria falhar")
	}
	for _, leak := range []string{unitToken, unitSecret} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("o erro do Notify vaza %q: %q", leak, err)
		}
	}
	if !errors.Is(err, bot.ErrorForbidden) {
		t.Error("a cadeia (bot.ErrorForbidden) se perdeu")
	}
}

// Um erro de decodificação da resposta embute o corpo bruto: o Notify devolve
// mensagem fixa, com a causa só na cadeia.
func TestNotifyDecodeErrorDoesNotEchoTheRawBody(t *testing.T) {
	c := &Client{token: unitToken, send: func(context.Context, *bot.SendMessageParams) error {
		return fmt.Errorf("error decode response body for method sendMessage, %s, %w", "marcador-corpo-bruto", json.Unmarshal([]byte("{"), new(any)))
	}}

	err := c.Notifier().Notify(context.Background(), 7, "oi")

	if err == nil || strings.Contains(err.Error(), "marcador-corpo-bruto") {
		t.Errorf("o erro deveria existir e não conter o corpo bruto: %v", err)
	}
	var syn *json.SyntaxError
	if !errors.As(err, &syn) {
		t.Error("a causa (json.SyntaxError) deveria seguir na cadeia")
	}
}
