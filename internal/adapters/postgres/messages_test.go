package postgres_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/duvrdx/noto/internal/adapters/postgres"
	"github.com/duvrdx/noto/internal/core/message"
	"github.com/duvrdx/noto/internal/core/ports"
	"github.com/duvrdx/noto/internal/testutil/testdb"
)

// Compile-time: o repositório é a porta do core.
var _ ports.MessageStore = (*postgres.MessageStore)(nil)

// querier é o que o pool e a transação de teste têm em comum para as consultas.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func count(t *testing.T, q querier, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := q.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func incoming(updateID int64, text string) message.Incoming {
	return message.Incoming{UpdateID: updateID, TelegramUserID: 42, ChatID: 7, Text: text}
}

func TestSaveIncomingNewMessage(t *testing.T) {
	tx := testdb.Tx(t)
	store := postgres.NewMessageStore(tx)
	ctx := context.Background()

	res, err := store.SaveIncoming(ctx, message.Incoming{UpdateID: 100, TelegramUserID: 42, ChatID: 7, Text: "oi"})

	if err != nil {
		t.Fatalf("SaveIncoming: %v", err)
	}
	if !res.Created {
		t.Error("Created = false, want true")
	}
	if count(t, tx, `SELECT count(*) FROM users`) != 1 || count(t, tx, `SELECT count(*) FROM messages`) != 1 {
		t.Fatal("esperava uma linha em users e uma em messages")
	}
	var userID, messageID, text string
	var telegramUser, updateID, chatID int64
	err = tx.QueryRow(ctx, `
		SELECT u.id::text, m.id::text, u.telegram_user_id, m.telegram_update_id, m.chat_id, m.raw_text
		FROM messages m JOIN users u ON u.id = m.user_id`).Scan(&userID, &messageID, &telegramUser, &updateID, &chatID, &text)
	if err != nil {
		t.Fatal(err)
	}
	if telegramUser != 42 || updateID != 100 || chatID != 7 || text != "oi" {
		t.Errorf("linha = user %d update %d chat %d texto %q", telegramUser, updateID, chatID, text)
	}
	if res.UserID != userID || res.MessageID != messageID {
		t.Errorf("SaveResult ids = (%q, %q), linhas = (%q, %q)", res.UserID, res.MessageID, userID, messageID)
	}
}

func TestSaveIncomingSameUserReusesRow(t *testing.T) {
	tx := testdb.Tx(t)
	store := postgres.NewMessageStore(tx)
	ctx := context.Background()

	first, err := store.SaveIncoming(ctx, incoming(1, "primeira"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveIncoming(ctx, incoming(2, "segunda"))
	if err != nil {
		t.Fatal(err)
	}

	if !second.Created {
		t.Error("segunda mensagem: Created = false, want true")
	}
	if second.UserID != first.UserID {
		t.Errorf("UserID mudou: %q -> %q", first.UserID, second.UserID)
	}
	if n := count(t, tx, `SELECT count(*) FROM users`); n != 1 {
		t.Errorf("users = %d linhas, want 1", n)
	}
	if n := count(t, tx, `SELECT count(*) FROM messages WHERE user_id = $1::uuid`, first.UserID); n != 2 {
		t.Errorf("messages do usuário = %d, want 2", n)
	}
}

// Premissa (b): pgx.Tx.Begin cria savepoint, então SaveIncoming abre a "sua"
// transação por dentro da transação do testdb.Tx. O que ele grava aqui some
// no rollback do teste: este teste, rodando depois do anterior, não vê nada.
func TestSaveIncomingInsideTestTxLeavesNothingBehind(t *testing.T) {
	tx := testdb.Tx(t)

	if n := count(t, tx, `SELECT count(*) FROM users`) + count(t, tx, `SELECT count(*) FROM messages`); n != 0 {
		t.Errorf("sobrou %d linha(s) de um teste anterior: o rollback do testdb.Tx não isolou", n)
	}
}

func TestSaveIncomingDuplicateUpdateIsNotAnError(t *testing.T) {
	tx := testdb.Tx(t)
	store := postgres.NewMessageStore(tx)
	ctx := context.Background()

	first, err := store.SaveIncoming(ctx, incoming(100, "oi"))
	if err != nil {
		t.Fatal(err)
	}

	dup, err := store.SaveIncoming(ctx, incoming(100, "oi"))

	if err != nil {
		t.Fatalf("update repetido não pode ser erro: %v", err)
	}
	if dup.Created {
		t.Error("Created = true para update repetido")
	}
	if dup.UserID != first.UserID {
		t.Errorf("UserID do duplicado = %q, want %q", dup.UserID, first.UserID)
	}
	if n := count(t, tx, `SELECT count(*) FROM messages WHERE telegram_update_id = 100`); n != 1 {
		t.Errorf("messages com update 100 = %d, want 1", n)
	}
	if n := count(t, tx, `SELECT count(*) FROM users`); n != 1 {
		t.Errorf("users = %d, want 1", n)
	}
}

// Oito chamadas simultâneas do mesmo update, com commit real e conexões
// separadas: exatamente uma cria a linha.
func TestSaveIncomingConcurrentSameUpdateCreatesExactlyOnce(t *testing.T) {
	pool := testdb.Pool(t)
	store := postgres.NewMessageStore(pool)
	const n = 8

	results := make([]ports.SaveResult, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = store.SaveIncoming(context.Background(), incoming(500, "concorrente"))
		}()
	}
	close(start)
	wg.Wait()

	created := 0
	for i := range n {
		if errs[i] != nil {
			t.Errorf("chamada %d: %v", i, errs[i])
		}
		if results[i].Created {
			created++
		}
	}
	if created != 1 {
		t.Errorf("%d chamadas devolveram Created=true, want exatamente 1", created)
	}
	if got := count(t, pool, `SELECT count(*) FROM messages WHERE telegram_update_id = 500`); got != 1 {
		t.Errorf("messages com update 500 = %d, want 1", got)
	}
	if got := count(t, pool, `SELECT count(*) FROM users`); got != 1 {
		t.Errorf("users = %d, want 1", got)
	}
}

// Updates diferentes do mesmo usuário novo, ao mesmo tempo: uma linha em
// users e nenhuma violação de unicidade em nenhuma chamada.
func TestSaveIncomingConcurrentNewUserCreatesOneUser(t *testing.T) {
	pool := testdb.Pool(t)
	store := postgres.NewMessageStore(pool)
	const n = 8

	results := make([]ports.SaveResult, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = store.SaveIncoming(context.Background(), incoming(int64(600+i), "mesmo usuário"))
		}()
	}
	close(start)
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Errorf("chamada %d: %v", i, errs[i])
			continue
		}
		if !results[i].Created {
			t.Errorf("chamada %d: Created = false, os updates são distintos", i)
		}
		if results[i].UserID != results[0].UserID {
			t.Errorf("chamada %d: UserID %q difere de %q", i, results[i].UserID, results[0].UserID)
		}
	}
	if got := count(t, pool, `SELECT count(*) FROM users`); got != 1 {
		t.Errorf("users = %d, want 1", got)
	}
	if got := count(t, pool, `SELECT count(*) FROM messages`); got != n {
		t.Errorf("messages = %d, want %d", got, n)
	}
}

func TestSaveIncomingPreservesTextByteForByte(t *testing.T) {
	long := strings.Repeat("ação-ü ", 4096/7+1)
	long = string([]rune(long)[:4096])
	tests := map[string]string{
		"acentos":            "áéíóú ç ã õ ñ ü",
		"emoji":              "🚀 🇧🇷 👨‍👩‍👧 ✅",
		"quebras de linha":   "linha 1\nlinha 2\r\n\tlinha 3\n\n",
		"espaços nas pontas": "  com espaços  ",
		"4096 caracteres":    long,
	}
	if n := len([]rune(long)); n != 4096 {
		t.Fatalf("texto longo de teste tem %d caracteres, want 4096", n)
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			tx := testdb.Tx(t)
			store := postgres.NewMessageStore(tx)

			if _, err := store.SaveIncoming(context.Background(), incoming(1, text)); err != nil {
				t.Fatal(err)
			}

			var got string
			if err := tx.QueryRow(context.Background(), `SELECT raw_text FROM messages`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != text {
				t.Errorf("raw_text difere do recebido: got %d bytes, want %d bytes", len(got), len(text))
			}
		})
	}
}

// Incoming inválida devolve erro sem escrever nada, e o erro nunca traz o texto.
func TestSaveIncomingInvalidWritesNothing(t *testing.T) {
	const marker = "marcador-segredo"
	tests := map[string]message.Incoming{
		"UpdateID zero":        {UpdateID: 0, TelegramUserID: 42, ChatID: 7, Text: marker},
		"TelegramUserID zero":  {UpdateID: 1, TelegramUserID: 0, ChatID: 7, Text: marker},
		"ChatID zero":          {UpdateID: 1, TelegramUserID: 42, ChatID: 0, Text: marker},
		"texto vazio":          {UpdateID: 1, TelegramUserID: 42, ChatID: 7, Text: ""},
		"texto com byte nulo":  {UpdateID: 1, TelegramUserID: 42, ChatID: 7, Text: marker + "\x00"},
		"texto UTF-8 inválido": {UpdateID: 1, TelegramUserID: 42, ChatID: 7, Text: marker + "\xff"},
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			tx := testdb.Tx(t)
			store := postgres.NewMessageStore(tx)

			_, err := store.SaveIncoming(context.Background(), in)

			if err == nil {
				t.Fatal("Incoming inválida deveria devolver erro")
			}
			if strings.Contains(err.Error(), marker) {
				t.Errorf("erro contém o texto do usuário: %q", err)
			}
			if n := count(t, tx, `SELECT count(*) FROM users`) + count(t, tx, `SELECT count(*) FROM messages`); n != 0 {
				t.Errorf("escreveu %d linha(s) apesar da entrada inválida", n)
			}
		})
	}
}

// Falhas de banco também não carregam o texto.
func TestSaveIncomingDatabaseErrorNeverContainsText(t *testing.T) {
	const marker = "marcador-segredo"

	t.Run("pool fechado", func(t *testing.T) {
		pool := testdb.Pool(t)
		store := postgres.NewMessageStore(pool)
		pool.Close()

		_, err := store.SaveIncoming(context.Background(), incoming(1, marker))

		if err == nil {
			t.Fatal("pool fechado deveria dar erro")
		}
		if strings.Contains(err.Error(), marker) {
			t.Errorf("erro contém o texto do usuário: %q", err)
		}
	})
	t.Run("contexto cancelado", func(t *testing.T) {
		store := postgres.NewMessageStore(testdb.Pool(t))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := store.SaveIncoming(ctx, incoming(1, marker))

		if err == nil {
			t.Fatal("contexto cancelado deveria dar erro")
		}
		if strings.Contains(err.Error(), marker) {
			t.Errorf("erro contém o texto do usuário: %q", err)
		}
	})
}
