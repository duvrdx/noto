// Package notifiertest é o dublê de ports.Notifier dos testes (testing.md §2):
// um coletor em memória, seguro para uso concorrente, que permite afirmar
// *o que* seria enviado sem nenhuma rede. É código de teste: nenhum pacote de
// produção o importa.
package notifiertest

import (
	"context"
	"sync"

	"github.com/duvrdx/noto/internal/core/ports"
)

var _ ports.Notifier = (*Notifier)(nil)

// Call é uma chamada registrada a Notify.
type Call struct {
	ChatID int64
	Text   string
}

// Notifier registra as chamadas a Notify. Zero value é um coletor que não
// falha; Failing devolve a variante que responde erro.
type Notifier struct {
	mu    sync.Mutex
	calls []Call
	err   error
	hook  func(ctx context.Context, chatID int64, text string)
}

// New devolve um coletor cujo Notify sempre tem sucesso.
func New() *Notifier { return &Notifier{} }

// Failing devolve um coletor cujo Notify registra a chamada e devolve err.
func Failing(err error) *Notifier { return &Notifier{err: err} }

// OnNotify registra fn para rodar, de forma síncrona, dentro de cada Notify
// (antes de registrar a chamada), sem segurar o mutex do coletor: serve para
// o teste inspecionar o estado do mundo no instante do envio.
func (n *Notifier) OnNotify(fn func(ctx context.Context, chatID int64, text string)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.hook = fn
}

// Notify registra a chamada e devolve o erro configurado (nil no coletor comum).
func (n *Notifier) Notify(ctx context.Context, chatID int64, text string) error {
	n.mu.Lock()
	hook := n.hook
	n.mu.Unlock()
	if hook != nil {
		hook(ctx, chatID, text)
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, Call{ChatID: chatID, Text: text})
	return n.err
}

// Calls devolve uma cópia das chamadas, na ordem em que foram registradas.
func (n *Notifier) Calls() []Call {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]Call(nil), n.calls...)
}
