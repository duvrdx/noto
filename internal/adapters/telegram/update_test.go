package telegram_test

import (
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/duvrdx/noto/internal/adapters/telegram"
	"github.com/duvrdx/noto/internal/core/message"
)

// marker é um texto único: nenhum motivo de descarte pode contê-lo.
const marker = "marcador-mapeamento-unico"

func msg(chatType models.ChatType, from *models.User, text string) *models.Message {
	return &models.Message{
		ID:   1,
		Chat: models.Chat{ID: 7, Type: chatType},
		From: from,
		Text: text,
	}
}

var alice = &models.User{ID: 42, FirstName: "Alice"}

func TestMapUpdate(t *testing.T) {
	tests := []struct {
		name   string
		update *models.Update
		want   message.Incoming
		reason telegram.DiscardReason // vazio = entregue
	}{
		{
			name:   "texto privado mapeia os quatro campos",
			update: &models.Update{ID: 100, Message: msg(models.ChatTypePrivate, alice, "oi")},
			want:   message.Incoming{UpdateID: 100, TelegramUserID: 42, ChatID: 7, Text: "oi"},
		},
		{
			name:   "/start é texto comum",
			update: &models.Update{ID: 101, Message: msg(models.ChatTypePrivate, alice, "/start")},
			want:   message.Incoming{UpdateID: 101, TelegramUserID: 42, ChatID: 7, Text: "/start"},
		},
		{
			name:   "outro comando também é texto comum",
			update: &models.Update{ID: 102, Message: msg(models.ChatTypePrivate, alice, "/hoje agora")},
			want:   message.Incoming{UpdateID: 102, TelegramUserID: 42, ChatID: 7, Text: "/hoje agora"},
		},
		{
			// O mapeamento não reinterpreta o texto: quem recusa NUL, UTF-8
			// inválido e o resto é Incoming.Validate.
			name:   "texto com byte nulo passa adiante",
			update: &models.Update{ID: 103, Message: msg(models.ChatTypePrivate, alice, "a\x00b")},
			want:   message.Incoming{UpdateID: 103, TelegramUserID: 42, ChatID: 7, Text: "a\x00b"},
		},
		{
			name:   "texto com UTF-8 inválido passa adiante",
			update: &models.Update{ID: 104, Message: msg(models.ChatTypePrivate, alice, "a\xffb")},
			want:   message.Incoming{UpdateID: 104, TelegramUserID: 42, ChatID: 7, Text: "a\xffb"},
		},
		{
			name:   "texto só de espaços passa adiante",
			update: &models.Update{ID: 105, Message: msg(models.ChatTypePrivate, alice, "  \n ")},
			want:   message.Incoming{UpdateID: 105, TelegramUserID: 42, ChatID: 7, Text: "  \n "},
		},
		{
			name:   "sem message",
			update: &models.Update{ID: 110},
			reason: telegram.ReasonNoMessage,
		},
		{
			name:   "edited_message não é message",
			update: &models.Update{ID: 111, EditedMessage: msg(models.ChatTypePrivate, alice, marker)},
			reason: telegram.ReasonNoMessage,
		},
		{
			name:   "channel_post não é message",
			update: &models.Update{ID: 112, ChannelPost: msg(models.ChatTypeChannel, nil, marker)},
			reason: telegram.ReasonNoMessage,
		},
		{
			name:   "callback_query não é message",
			update: &models.Update{ID: 113, CallbackQuery: &models.CallbackQuery{ID: "x", Data: marker}},
			reason: telegram.ReasonNoMessage,
		},
		{
			name:   "sem from",
			update: &models.Update{ID: 120, Message: msg(models.ChatTypePrivate, nil, marker)},
			reason: telegram.ReasonNoSender,
		},
		{
			name:   "grupo",
			update: &models.Update{ID: 130, Message: msg(models.ChatTypeGroup, alice, marker)},
			reason: telegram.ReasonNotPrivate,
		},
		{
			name:   "supergrupo",
			update: &models.Update{ID: 131, Message: msg(models.ChatTypeSupergroup, alice, marker)},
			reason: telegram.ReasonNotPrivate,
		},
		{
			name:   "canal",
			update: &models.Update{ID: 132, Message: msg(models.ChatTypeChannel, alice, marker)},
			reason: telegram.ReasonNotPrivate,
		},
		{
			name: "foto sem texto",
			update: &models.Update{ID: 140, Message: &models.Message{
				Chat: models.Chat{ID: 7, Type: models.ChatTypePrivate}, From: alice,
				Photo: []models.PhotoSize{{FileID: "f", Width: 1, Height: 1}},
			}},
			reason: telegram.ReasonNoText,
		},
		{
			name: "foto com legenda não é texto",
			update: &models.Update{ID: 141, Message: &models.Message{
				Chat: models.Chat{ID: 7, Type: models.ChatTypePrivate}, From: alice,
				Photo: []models.PhotoSize{{FileID: "f", Width: 1, Height: 1}}, Caption: marker,
			}},
			reason: telegram.ReasonNoText,
		},
		{
			name: "sticker",
			update: &models.Update{ID: 142, Message: &models.Message{
				Chat: models.Chat{ID: 7, Type: models.ChatTypePrivate}, From: alice,
				Sticker: &models.Sticker{FileID: "s"},
			}},
			reason: telegram.ReasonNoText,
		},
		{
			name: "voz",
			update: &models.Update{ID: 143, Message: &models.Message{
				Chat: models.Chat{ID: 7, Type: models.ChatTypePrivate}, From: alice,
				Voice: &models.Voice{FileID: "v"},
			}},
			reason: telegram.ReasonNoText,
		},
		{
			name:   "texto vazio",
			update: &models.Update{ID: 144, Message: msg(models.ChatTypePrivate, alice, "")},
			reason: telegram.ReasonNoText,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := telegram.MapUpdate(tt.update)

			if reason != tt.reason {
				t.Fatalf("motivo = %q, want %q", reason, tt.reason)
			}
			if tt.reason != "" {
				if got != (message.Incoming{}) {
					t.Errorf("descartado deveria devolver Incoming zerada, veio %+v", got)
				}
				// O motivo vai para o log: constante, nunca o texto.
				if strings.Contains(string(reason), marker) {
					t.Errorf("o motivo contém o texto: %q", reason)
				}
				return
			}
			if got != tt.want {
				t.Errorf("Incoming = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMapUpdateNilUpdate(t *testing.T) {
	if _, reason := telegram.MapUpdate(nil); reason != telegram.ReasonNoMessage {
		t.Errorf("update nulo: motivo = %q, want %q", reason, telegram.ReasonNoMessage)
	}
}

// Os motivos são distintos entre si (o log precisa distinguir o descarte).
func TestDiscardReasonsAreDistinctAndNonEmpty(t *testing.T) {
	seen := map[telegram.DiscardReason]bool{}
	for _, r := range []telegram.DiscardReason{
		telegram.ReasonNoMessage, telegram.ReasonNoSender, telegram.ReasonNotPrivate, telegram.ReasonNoText,
	} {
		if r == "" || seen[r] {
			t.Errorf("motivo vazio ou repetido: %q", r)
		}
		seen[r] = true
	}
}
