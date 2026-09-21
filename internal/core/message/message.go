// Package message define a mensagem de entrada do domínio: o que o Noto
// recebeu de um usuário, já livre de qualquer tipo de transporte.
package message

import (
	"errors"
	"unicode/utf8"
)

// Incoming é uma mensagem de texto recebida. Nada aqui é específico do
// Telegram além dos nomes dos identificadores.
type Incoming struct {
	UpdateID       int64  // id do update; chave de idempotência
	TelegramUserID int64  // dono da mensagem
	ChatID         int64  // para onde a resposta vai
	Text           string // conteúdo, preservado byte a byte
}

// Validate confere as invariantes da entrada. Os erros nomeiam o campo e
// nunca o conteúdo: o texto do usuário não pode chegar a log nem a erro.
//
// O domínio não reinterpreta o texto (espaços contam) nem assume chat
// privado (ChatID negativo é válido; filtrar chat é do adapter). O que ele
// recusa em Text além do vazio é o que o armazenamento não representa: byte
// nulo e UTF-8 inválido, que o tipo text do Postgres rejeita.
func (m Incoming) Validate() error {
	switch {
	case m.UpdateID == 0:
		return errors.New("mensagem inválida: UpdateID não pode ser zero")
	case m.TelegramUserID == 0:
		return errors.New("mensagem inválida: TelegramUserID não pode ser zero")
	case m.ChatID == 0:
		return errors.New("mensagem inválida: ChatID não pode ser zero")
	case m.Text == "":
		return errors.New("mensagem inválida: Text não pode ser vazio")
	case !utf8.ValidString(m.Text):
		return errors.New("mensagem inválida: Text não é UTF-8 válido")
	}
	for i := 0; i < len(m.Text); i++ {
		if m.Text[i] == 0 {
			return errors.New("mensagem inválida: Text não pode conter byte nulo")
		}
	}
	return nil
}
