package telegram

// Este arquivo concentra o caminho de segurança do adaptador: como um erro é
// CLASSIFICADO para o log (nunca o texto dele), como o TOKEN é REDIGIDO de todo
// erro que sai do pacote, e como o log é contido quando a mesma falha se
// repete. Regras:
//
//   - erros são registrados por classificação (errClass) e identificadores;
//     err.Error() nunca vai para o log;
//   - todo erro devolvido por NewClient e Notify passa por redact;
//   - a classificação usa errors.Is/errors.As sobre os tipos da biblioteca.
//     Texto de erro só é lido em duas funções, marcadas abaixo, e apenas para
//     classificar (nunca para logar).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/go-telegram/bot"
)

// errClass é a classificação de um erro, o que se registra no lugar do texto.
type errClass string

const (
	classCanceled     errClass = "contexto_cancelado"
	classNetwork      errClass = "rede"
	classConflict     errClass = "conflito"
	classRateLimit    errClass = "limite_de_taxa"
	classUnauthorized errClass = "nao_autorizado"
	classForbidden    errClass = "proibido"
	classBadRequest   errClass = "requisicao_invalida"
	classDecode       errClass = "decodificacao"
	classOther        errClass = "outro"
)

// classify diz de que tipo é o erro, pelos tipos da biblioteca e da stdlib
// (errors.Is e errors.As), nunca pelo texto, com uma única exceção
// isolada: o erro de decodificação sem tipo próprio (ver hasDecodePrefix).
func classify(err error) errClass {
	var (
		tooMany   *bot.TooManyRequestsError
		migrate   *bot.MigrateError
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
		netErr    net.Error
	)
	switch {
	case errors.Is(err, context.Canceled):
		return classCanceled
	case errors.Is(err, bot.ErrorConflict):
		return classConflict
	case errors.Is(err, bot.ErrorUnauthorized):
		return classUnauthorized
	case errors.Is(err, bot.ErrorForbidden):
		return classForbidden
	case errors.Is(err, bot.ErrorBadRequest), errors.As(err, &migrate):
		return classBadRequest
	case errors.Is(err, bot.ErrorTooManyRequests), errors.As(err, &tooMany):
		return classRateLimit
	case errors.As(err, &syntaxErr), errors.As(err, &typeErr), hasDecodePrefix(err):
		return classDecode
	case errors.As(err, &netErr), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return classNetwork
	default:
		return classOther
	}
}

// decodeUpdatePrefix abre as mensagens que a biblioteca monta quando um update
// não decodifica ("error decode update, <corpo>, <erro>" e "error decode update
// <id>, skipped, <corpo>, <erro>"). O caso sem update_id (errMissingUpdateID) não
// tem tipo exportado para errors.Is, então o prefixo é a única forma de
// reconhecê-lo.
const decodeUpdatePrefix = "error decode update"

// hasDecodePrefix lê o TEXTO do erro só para classificar: o texto contém o
// corpo bruto do update (com o texto do usuário) e por isso nunca é logado.
func hasDecodePrefix(err error) bool {
	return strings.HasPrefix(err.Error(), decodeUpdatePrefix)
}

// decodeErrorUpdateID extrai o update_id de "error decode update <id>, skipped, ...".
// Lê o texto do erro só para isso: devolve o número e nada mais do texto.
func decodeErrorUpdateID(err error) (int64, bool) {
	rest, ok := strings.CutPrefix(err.Error(), decodeUpdatePrefix+" ")
	if !ok {
		return 0, false
	}
	digits := rest
	if i := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		digits = rest[:i]
	}
	if digits == "" || len(digits) == len(rest) || rest[len(digits)] != ',' {
		return 0, false
	}
	id, err := strconv.ParseInt(digits, 10, 64)
	return id, err == nil
}

// --- Redação do token ---------------------------------------------------------

const redacted = "[REDACTED]"

// minSecretLen evita redigir um "segredo" tão curto que casaria com texto comum.
const minSecretLen = 8

// sanitizedError tem uma mensagem própria (redigida ou fixa) e mantém o erro
// original na cadeia, então errors.Is e errors.As continuam funcionando.
type sanitizedError struct {
	msg string
	err error
}

func (e *sanitizedError) Error() string { return e.msg }
func (e *sanitizedError) Unwrap() error { return e.err }

// redact troca toda ocorrência do token na mensagem do erro por [REDACTED]:
// o token inteiro (<id>:<segredo>), o <segredo> isolado (a URL pode ser
// cortada ou ter o ":" codificado) e as formas com percent-encoding. A forma
// "bot<token>" do caminho da URL é coberta pelo token inteiro. O erro
// original segue na cadeia (Unwrap), então a checagem de tipo continua
// possível; quem formata o erro devolvido vê só a mensagem redigida. Sem o
// token na mensagem, devolve o próprio erro.
func redact(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	pairs := []string{token, redacted, url.QueryEscape(token), redacted, url.PathEscape(token), redacted}
	if _, secret, ok := strings.Cut(token, ":"); ok && len(secret) >= minSecretLen {
		pairs = append(pairs, secret, redacted, url.QueryEscape(secret), redacted)
	}
	msg := err.Error()
	out := strings.NewReplacer(pairs...).Replace(msg)
	if out == msg {
		return err
	}
	return &sanitizedError{msg: out, err: err}
}

// --- Registro de erros sem spam -----------------------------------------------

// reporter registra falhas do polling por classificação. getUpdates falha em
// laço, com espera crescente: sem contenção, um 409 ou uma rede caída geraria
// uma linha por tentativa, por horas. Registra a primeira falha de uma classe,
// suprime as repetições consecutivas da mesma classe (volta a registrar na 10ª,
// 100ª, 1000ª... com o contador) e registra "recuperado" quando o polling volta a
// funcionar. Limitação conhecida: alternar entre duas classes registra a cada
// troca.
type reporter struct {
	log *slog.Logger

	mu    sync.Mutex
	last  errClass // classe da sequência de falhas em curso; "" = sem falha pendente
	count int      // falhas consecutivas dessa classe
}

func newReporter(log *slog.Logger) *reporter { return &reporter{log: log} }

func (r *reporter) failure(class errClass, level slog.Level, msg string, attrs ...any) {
	r.mu.Lock()
	if class == r.last {
		r.count++
	} else {
		r.last, r.count = class, 1
	}
	n := r.count
	r.mu.Unlock()

	if n != 1 && !(n >= 10 && isPowerOfTen(n)) {
		return
	}
	attrs = append(attrs, "classe", string(class))
	if n > 1 {
		attrs = append(attrs, "repeticoes", n)
	}
	r.log.Log(context.Background(), level, msg, attrs...)
}

// recovered registra a volta ao normal, se havia falha pendente.
func (r *reporter) recovered() {
	r.mu.Lock()
	prev, n := r.last, r.count
	r.last, r.count = "", 0
	r.mu.Unlock()
	if prev == "" {
		return
	}
	r.log.Info("recuperado", "classe_anterior", string(prev), "falhas_consecutivas", n)
}

func isPowerOfTen(n int) bool {
	for n >= 10 && n%10 == 0 {
		n /= 10
	}
	return n == 1
}

// onLibraryError é o manipulador de erros da biblioteca: registra sempre por
// classificação (mais o update_id, quando o erro o traz, ou o retry_after), com
// uma mensagem fixa e acionável. Nunca o texto do erro: ele pode trazer o
// corpo bruto de um update.
func (c *Client) onLibraryError(err error) {
	class := classify(err)
	// Com o Run encerrando, o que a biblioteca reclama (updates que não couberam
	// no canal, requisição abortada) é consequência do cancelamento.
	if c.runCtx != nil && c.runCtx.Err() != nil {
		class = classCanceled
	}

	level, msg := slog.LevelError, "erro na API do Telegram"
	var attrs []any
	switch class {
	case classCanceled:
		level, msg = slog.LevelDebug, "polling interrompido pelo encerramento"
	case classConflict:
		msg = "conflito no getUpdates: provavelmente há um webhook ativo ou outra instância consultando o mesmo bot " +
			"(use um bot por ambiente); o polling continua tentando"
	case classUnauthorized:
		msg = "o Telegram rejeitou o token durante o polling (revogado?)"
	case classRateLimit:
		msg = "limite de taxa do Telegram no polling"
		var tooMany *bot.TooManyRequestsError
		if errors.As(err, &tooMany) {
			attrs = append(attrs, "retry_after_s", tooMany.RetryAfter)
		}
	case classDecode:
		msg = "update recebido não pôde ser decodificado e foi descartado"
		if id, ok := decodeErrorUpdateID(err); ok {
			attrs = append(attrs, "update_id", id)
		}
	case classNetwork:
		msg = "falha de rede ao consultar o Telegram"
	}
	c.reporter.failure(class, level, msg, attrs...)
}

// pollSucceeded é chamado quando uma consulta a getUpdates volta com sucesso.
func (c *Client) pollSucceeded() { c.reporter.recovered() }
