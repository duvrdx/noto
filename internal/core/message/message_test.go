package message_test

import (
	"strings"
	"testing"

	"github.com/duvrdx/noto/internal/core/message"
)

func valid() message.Incoming {
	return message.Incoming{UpdateID: 100, TelegramUserID: 42, ChatID: 7, Text: "oi"}
}

func TestIncomingValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*message.Incoming)
		wantErr string // campo que o erro deve nomear; vazio = válido
	}{
		{name: "válido", mutate: func(*message.Incoming) {}},
		{name: "UpdateID zero", mutate: func(m *message.Incoming) { m.UpdateID = 0 }, wantErr: "UpdateID"},
		{name: "TelegramUserID zero", mutate: func(m *message.Incoming) { m.TelegramUserID = 0 }, wantErr: "TelegramUserID"},
		{name: "ChatID zero", mutate: func(m *message.Incoming) { m.ChatID = 0 }, wantErr: "ChatID"},
		{name: "Text vazio", mutate: func(m *message.Incoming) { m.Text = "" }, wantErr: "Text"},
		// O domínio não reinterpreta o que o usuário digitou.
		{name: "Text só de espaços é válido", mutate: func(m *message.Incoming) { m.Text = "   \n\t " }},
		// O domínio não assume chat privado; isso é filtro do adapter.
		{name: "ChatID negativo é válido", mutate: func(m *message.Incoming) { m.ChatID = -1001234567890 }},
		{name: "acentos, emoji e quebras de linha", mutate: func(m *message.Incoming) { m.Text = "ação 🚀\nlinha 2" }},
		// O Postgres rejeita NUL e UTF-8 inválido em text: recusar aqui evita
		// um erro obscuro do banco no meio da transação.
		{name: "Text com byte nulo", mutate: func(m *message.Incoming) { m.Text = "marcador-segredo\x00fim" }, wantErr: "Text"},
		{name: "Text com UTF-8 inválido", mutate: func(m *message.Incoming) { m.Text = "marcador-segredo\xfffim" }, wantErr: "Text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := valid()
			tt.mutate(&m)

			err := m.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, queria erro citando %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("erro %q não nomeia o campo %q", err, tt.wantErr)
			}
			// O erro nomeia o campo, nunca o conteúdo (texto do usuário não vai a log).
			if strings.Contains(err.Error(), "marcador-segredo") {
				t.Errorf("erro vaza o texto do usuário: %q", err)
			}
		})
	}
}
