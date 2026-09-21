package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/duvrdx/noto/internal/core/message"
)

const (
	// defaultAPIBaseURL é a API de produção; os testes apontam APIBaseURL para o tgfake.
	defaultAPIBaseURL = "https://api.telegram.org"

	// handlerTimeout é o prazo de cada update sob contexto próprio: menor que
	// os 10 s de stop_grace_period do Compose, para um "docker compose stop"
	// durante um update não virar SIGKILL.
	handlerTimeout = 8 * time.Second

	// classHandler classifica, no log, a falha devolvida pelo handler.
	classHandler = "handler"
)

// Config configura o Client.
type Config struct {
	Token      string       // TELEGRAM_BOT_TOKEN
	APIBaseURL string       // vazio = API de produção; os testes apontam ao tgfake
	Log        *slog.Logger // injetado; nil descarta os logs
}

// sender envia uma mensagem pela API.
type sender func(ctx context.Context, params *bot.SendMessageParams) error

// Handler processa uma mensagem de entrada. O contexto é próprio do update
// (não é cancelado pelo encerramento do Run) e tem prazo de 8 s.
type Handler func(ctx context.Context, in message.Incoming) error

// Client é o polling do Telegram: consulta getUpdates, mapeia cada update e o
// entrega, serial e em ordem, ao Handler.
type Client struct {
	bot      *bot.Bot
	log      *slog.Logger
	token    string    // só para redigir erros; nunca logado
	reporter *reporter // erros do polling, por classificação e sem spam
	send     sender    // sendMessage; um campo para o teste alcançar o caminho de erro do Notifier

	// Definidos por Run antes de bot.Start; lidos só pelas goroutines que o
	// Start lança depois.
	runCtx  context.Context
	handler Handler
}

// NewClient constrói o cliente e valida o token com getMe (o Telegram é a
// validação autoritativa; o formato não é conferido). Token rejeitado devolve
// erro que diz isso, sem o valor do token.
//
// A configuração da biblioteca fecha por construção as saídas que ela teria
// por padrão: os três manipuladores (update, erro e debug) são substituídos,
// porque os padrões escrevem no pacote log global o update inteiro (com o
// texto do usuário) e o corpo bruto de erros; o modo debug nunca é ligado.
// A fila interna tem capacidade 1, com um worker e handlers não assíncronos:
// o poller só consulta de novo depois de entregar o lote, e o processamento
// é serial e em ordem.
func NewClient(cfg Config) (*Client, error) {
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	base := cfg.APIBaseURL
	if base == "" {
		base = defaultAPIBaseURL
	}

	c := &Client{log: log, token: cfg.Token, reporter: newReporter(log)}
	b, err := bot.New(cfg.Token,
		bot.WithHTTPClient(pollTimeout, pollObserver{inner: &http.Client{Timeout: pollTimeout}, ok: c.pollSucceeded}),
		bot.WithServerURL(base),
		bot.WithAllowedUpdates(bot.AllowedUpdates{models.AllowedUpdateMessage}),
		bot.WithUpdatesChannelCap(1),
		bot.WithWorkers(1),
		bot.WithNotAsyncHandlers(),
		bot.WithDefaultHandler(func(_ context.Context, _ *bot.Bot, u *models.Update) { c.dispatch(u) }),
		// Os erros da biblioteca são registrados por classificação (nunca por
		// err.Error(), que pode trazer o corpo bruto de um update). O debug nunca
		// é ligado; o manipulador existe só para o padrão (log global) não valer.
		bot.WithErrorsHandler(c.onLibraryError),
		bot.WithDebugHandler(func(string, ...any) {}),
	)
	if err != nil {
		// Todo erro que sai daqui passa por redact: a biblioteca só redige o
		// token no erro do Do (não no de NewRequest, por exemplo).
		if errors.Is(err, bot.ErrorUnauthorized) {
			return nil, redact(fmt.Errorf("token rejeitado pelo Telegram (getMe respondeu 401): confira TELEGRAM_BOT_TOKEN: %w", err), cfg.Token)
		}
		return nil, redact(fmt.Errorf("validar o token com getMe (%s): %w", classify(err), err), cfg.Token)
	}
	c.bot = b
	c.send = func(ctx context.Context, p *bot.SendMessageParams) error {
		_, err := b.SendMessage(ctx, p)
		return err
	}
	return c, nil
}

// pollTimeout é o do long polling (o padrão da biblioteca).
const pollTimeout = time.Minute

// pollObserver é o cliente HTTP da biblioteca com um aviso quando uma consulta
// a getUpdates volta com sucesso, para registrar a recuperação depois de erros.
type pollObserver struct {
	inner *http.Client
	ok    func()
}

func (o pollObserver) Do(req *http.Request) (*http.Response, error) {
	resp, err := o.inner.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK && strings.HasSuffix(req.URL.Path, "/getUpdates") {
		o.ok()
	}
	return resp, err
}

// Run consulta o Telegram e entrega cada update aceito ao handler, até o
// contexto ser cancelado. Ao cancelamento para de consultar, conclui o update
// em processamento (sob contexto próprio, que o cancelamento não afeta, com
// prazo de 8 s) e devolve nil. Roda uma vez por Client. Os erros da
// biblioteca durante o Run são registrados por classificação (onLibraryError) e
// não interrompem o polling; por isso Run não tem erro para devolver, nem para
// redigir.
func (c *Client) Run(ctx context.Context, handler Handler) error {
	c.runCtx = ctx
	c.handler = handler
	c.bot.Start(ctx)
	return nil
}

// dispatch trata um update recebido do poller: descarta o que não é texto
// privado (com log de update_id e motivo) ou entrega ao handler. O erro do
// handler é logado só com o update_id e a classificação, e não interrompe
// nada: o offset já avançou e o próximo update segue.
func (c *Client) dispatch(u *models.Update) {
	in, reason := MapUpdate(u)
	if reason != "" {
		c.log.Info("update descartado", "update_id", u.ID, "motivo", string(reason))
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.runCtx), handlerTimeout)
	defer cancel()
	if err := c.handler(ctx, in); err != nil {
		// Nunca err.Error(): o erro do handler pode embutir o texto do usuário.
		c.log.Error("falha ao processar o update", "update_id", in.UpdateID, "classe", classHandler)
	}
}
