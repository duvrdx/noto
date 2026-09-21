package telegram

import (
	"github.com/go-telegram/bot/models"

	"github.com/duvrdx/noto/internal/core/message"
)

// DiscardReason diz por que um update não vira message.Incoming. São
// constantes fixas, feitas para o log: nunca carregam o texto do usuário.
type DiscardReason string

const (
	// ReasonNoMessage: o update não é do tipo message (edição, canal, callback...).
	ReasonNoMessage DiscardReason = "sem_message"
	// ReasonNoSender: a mensagem não traz o remetente (from).
	ReasonNoSender DiscardReason = "sem_remetente"
	// ReasonNotPrivate: a mensagem não é de chat privado (grupo, canal...).
	ReasonNotPrivate DiscardReason = "chat_nao_privado"
	// ReasonNoText: a mensagem não tem texto (foto, sticker, voz, texto vazio).
	ReasonNoText DiscardReason = "sem_texto"
)

// MapUpdate converte um update do Telegram em message.Incoming. Só entrega
// mensagem (message) de texto não vazio, com remetente, em chat privado; de
// qualquer outro update devolve o motivo do descarte e uma Incoming zerada.
// É função pura: não depende de bot.Bot, e o motivo nunca contém o texto.
//
// O texto passa adiante como veio (comandos como /start são texto comum, e
// byte nulo ou UTF-8 inválido não são tratados aqui): quem valida o conteúdo
// é message.Incoming.Validate.
func MapUpdate(u *models.Update) (message.Incoming, DiscardReason) {
	if u == nil || u.Message == nil {
		return message.Incoming{}, ReasonNoMessage
	}
	m := u.Message
	if m.From == nil {
		return message.Incoming{}, ReasonNoSender
	}
	if m.Chat.Type != models.ChatTypePrivate {
		return message.Incoming{}, ReasonNotPrivate
	}
	if m.Text == "" {
		return message.Incoming{}, ReasonNoText
	}
	return message.Incoming{
		UpdateID:       u.ID,
		TelegramUserID: m.From.ID,
		ChatID:         m.Chat.ID,
		Text:           m.Text,
	}, ""
}
