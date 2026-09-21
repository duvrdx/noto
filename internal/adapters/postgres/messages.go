package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/duvrdx/noto/internal/adapters/postgres/db"
	"github.com/duvrdx/noto/internal/core/message"
	"github.com/duvrdx/noto/internal/core/ports"
)

// TxBeginner é o mínimo de que MessageStore precisa: abrir uma transação.
// *pgxpool.Pool a satisfaz; um pgx.Tx também (o Begin dele cria savepoint), o
// que deixa o helper de teste entregar uma transação com rollback e o
// repositório ainda abrir "a sua" por dentro.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// MessageStore implementa ports.MessageStore sobre Postgres.
type MessageStore struct {
	db TxBeginner
}

// NewMessageStore devolve o repositório sobre db.
func NewMessageStore(db TxBeginner) *MessageStore {
	return &MessageStore{db: db}
}

var _ ports.MessageStore = (*MessageStore)(nil)

// SaveIncoming grava o usuário e a mensagem numa única transação. O banco
// decide o que é novo: o INSERT com ON CONFLICT (telegram_update_id) DO
// NOTHING não devolve linha quando o update já estava gravado, e isso vira
// Created=false, sem erro. Vale sob concorrência: o segundo INSERT espera o
// primeiro commitar e então encontra o conflito.
//
// Os erros nunca incluem o texto da mensagem.
func (s *MessageStore) SaveIncoming(ctx context.Context, in message.Incoming) (ports.SaveResult, error) {
	if err := in.Validate(); err != nil {
		return ports.SaveResult{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ports.SaveResult{}, fmt.Errorf("iniciar transação: %w", err)
	}
	// Depois do Commit isto devolve pgx.ErrTxClosed, que não interessa.
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	userID, err := q.UpsertUser(ctx, in.TelegramUserID)
	if err != nil {
		return ports.SaveResult{}, fmt.Errorf("gravar usuário: %w", err)
	}
	res := ports.SaveResult{UserID: uuidString(userID)}

	messageID, err := q.InsertMessage(ctx, db.InsertMessageParams{
		UserID:           userID,
		TelegramUpdateID: in.UpdateID,
		ChatID:           in.ChatID,
		RawText:          in.Text,
	})
	switch {
	case err == nil:
		res.Created = true
		res.MessageID = uuidString(messageID)
	case errors.Is(err, pgx.ErrNoRows):
		// Update repetido: não é erro; o usuário existe de qualquer forma.
	default:
		return ports.SaveResult{}, fmt.Errorf("gravar mensagem: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ports.SaveResult{}, fmt.Errorf("confirmar transação: %w", err)
	}
	return res, nil
}

// uuidString converte o UUID do driver para texto; o core só conhece string.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return u.String()
}
