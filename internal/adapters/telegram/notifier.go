package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-telegram/bot"

	"github.com/duvrdx/noto/internal/core/ports"
)

// notifier implementa ports.Notifier sobre o mesmo bot do polling.
type notifier struct{ c *Client }

var _ ports.Notifier = (*notifier)(nil)

// Notifier devolve o ports.Notifier deste cliente. Pode ser usado antes e
// durante o Run.
func (c *Client) Notifier() ports.Notifier { return &notifier{c: c} }

// Notify envia text ao chat, em texto puro: sem parse_mode, então nenhum
// caractere do usuário vira formatação, e o texto sai como veio. Uma única
// tentativa, sem retentativa (a biblioteca não tem retentativa automática, e o
// eco do M1 é at-most-once). Usa o contexto recebido, que é o do update. O erro
// devolvido é classificado (403 diz que o usuário bloqueou o bot) e tem o token
// redigido; a cadeia continua alcançável por errors.Is e errors.As.
func (n *notifier) Notify(ctx context.Context, chatID int64, text string) error {
	err := n.c.send(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
	if err == nil {
		return nil
	}
	return redact(describeSendError(err), n.c.token)
}

// describeSendError dá ao erro do envio uma mensagem que diz o que houve.
func describeSendError(err error) error {
	switch class := classify(err); class {
	case classForbidden:
		return fmt.Errorf("enviar mensagem: o usuário bloqueou o bot ou nunca iniciou a conversa (403): %w", err)
	case classRateLimit:
		var tooMany *bot.TooManyRequestsError
		if errors.As(err, &tooMany) {
			return fmt.Errorf("enviar mensagem: limite de taxa do Telegram (retry_after=%d s): %w", tooMany.RetryAfter, err)
		}
		return fmt.Errorf("enviar mensagem: limite de taxa do Telegram: %w", err)
	case classBadRequest:
		return fmt.Errorf("enviar mensagem: requisição rejeitada pelo Telegram (400): %w", err)
	case classUnauthorized:
		return fmt.Errorf("enviar mensagem: token rejeitado pelo Telegram (401): %w", err)
	case classCanceled:
		return fmt.Errorf("enviar mensagem: cancelado: %w", err)
	case classNetwork:
		return fmt.Errorf("enviar mensagem: falha de rede: %w", err)
	case classDecode:
		// O texto do erro de decodificação embute o corpo bruto da resposta:
		// mensagem fixa, com a causa só na cadeia.
		return &sanitizedError{msg: "enviar mensagem: resposta do Telegram ilegível", err: err}
	default:
		return fmt.Errorf("enviar mensagem: erro da API do Telegram: %w", err)
	}
}
