package ports

import (
	"context"

	"github.com/duvrdx/noto/internal/core/message"
)

// MessageStore persiste a mensagem de entrada e o usuário dono dela.
type MessageStore interface {
	// SaveIncoming grava o usuário (criado na primeira mensagem, reaproveitado
	// depois) e a mensagem numa única transação. Reentregar um UpdateID já
	// gravado não é erro: devolve Created=false e nada muda.
	SaveIncoming(ctx context.Context, in message.Incoming) (SaveResult, error)
}

// SaveResult é o desfecho de SaveIncoming. Os ids são UUIDs em texto: o core
// não conhece o tipo do driver.
type SaveResult struct {
	UserID    string
	MessageID string
	Created   bool // false: o UpdateID já estava gravado
}

// Notifier envia texto puro a um chat.
type Notifier interface {
	Notify(ctx context.Context, chatID int64, text string) error
}
