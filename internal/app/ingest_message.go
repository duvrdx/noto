package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/duvrdx/noto/internal/core/message"
	"github.com/duvrdx/noto/internal/core/ports"
)

// Outcome é o desfecho de um Handle sem erro.
type Outcome int

const (
	// Stored: a mensagem era nova, foi persistida e a resposta foi enviada.
	Stored Outcome = iota + 1
	// Duplicate: o update já estava registrado; nada foi gravado nem enviado.
	Duplicate
)

func (o Outcome) String() string {
	switch o {
	case Stored:
		return "stored"
	case Duplicate:
		return "duplicate"
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

// Classificação dos erros nos logs. É o que se registra no lugar do texto do
// erro de uma porta, que pode embutir o texto do usuário.
const (
	classInvalidInput = "entrada_invalida"
	classPersistence  = "persistencia"
	classSend         = "envio"
)

// IngestMessage é o caso de uso do M1: persistir a mensagem recebida e
// responder com o eco. Depende só das portas do core.
type IngestMessage struct {
	store    ports.MessageStore
	notifier ports.Notifier
	log      *slog.Logger
}

// NewIngestMessage monta o caso de uso. log é injetado (nunca o logger global);
// nil descarta os logs.
func NewIngestMessage(store ports.MessageStore, notifier ports.Notifier, log *slog.Logger) *IngestMessage {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &IngestMessage{store: store, notifier: notifier, log: log}
}

// Handle valida a mensagem, persiste usuário e mensagem numa transação e, só
// se a mensagem for nova e depois do commit, pede ao Notifier que devolva o
// mesmo texto ao mesmo chat (eco literal).
//
// Reentrega de um update já registrado devolve (Duplicate, nil): nada é
// gravado e o Notifier não é chamado. O eco é at-most-once: se o envio falhar,
// a mensagem continua persistida, o erro é devolvido e não há retentativa; a
// reentrega do update será Duplicate.
//
// Os erros devolvidos embrulham (%w) o que a porta devolveu, então errors.Is
// e errors.As funcionam. Esse erro pode conter o que a porta pôs nele,
// inclusive o texto do usuário: quem chama NÃO deve registrá-lo cru em log
// (o adapter de Telegram os registra por classificação). Os logs deste caso de
// uso só levam identificadores e uma classificação, nunca o texto nem o erro.
func (u *IngestMessage) Handle(ctx context.Context, in message.Incoming) (Outcome, error) {
	if err := in.Validate(); err != nil {
		u.log.WarnContext(ctx, "mensagem recusada",
			"update_id", in.UpdateID, "classe", classInvalidInput)
		return 0, fmt.Errorf("validar mensagem: %w", err)
	}

	res, err := u.store.SaveIncoming(ctx, in)
	if err != nil {
		u.log.ErrorContext(ctx, "falha ao persistir a mensagem",
			"update_id", in.UpdateID, "chat_id", in.ChatID, "classe", classPersistence)
		return 0, fmt.Errorf("persistir mensagem: %w", err)
	}

	if !res.Created {
		u.log.InfoContext(ctx, "mensagem duplicada ignorada",
			"update_id", in.UpdateID, "user_id", res.UserID, "chat_id", in.ChatID)
		return Duplicate, nil
	}
	u.log.InfoContext(ctx, "mensagem nova registrada",
		"update_id", in.UpdateID, "user_id", res.UserID, "message_id", res.MessageID, "chat_id", in.ChatID)

	if err := u.notifier.Notify(ctx, in.ChatID, in.Text); err != nil {
		u.log.ErrorContext(ctx, "falha ao enviar a resposta",
			"update_id", in.UpdateID, "user_id", res.UserID, "message_id", res.MessageID,
			"chat_id", in.ChatID, "classe", classSend)
		return 0, fmt.Errorf("enviar resposta: %w", err)
	}
	return Stored, nil
}
