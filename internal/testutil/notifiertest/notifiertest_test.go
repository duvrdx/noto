package notifiertest_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/duvrdx/noto/internal/testutil/notifiertest"
)

func TestNotifierRecordsCallsInOrder(t *testing.T) {
	n := notifiertest.New()

	if err := n.Notify(context.Background(), 7, "um"); err != nil {
		t.Fatal(err)
	}
	if err := n.Notify(context.Background(), 8, "dois"); err != nil {
		t.Fatal(err)
	}

	got := n.Calls()
	want := []notifiertest.Call{{ChatID: 7, Text: "um"}, {ChatID: 8, Text: "dois"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Calls() = %v, want %v", got, want)
	}
}

func TestCallsReturnsACopy(t *testing.T) {
	n := notifiertest.New()
	_ = n.Notify(context.Background(), 1, "a")

	n.Calls()[0].Text = "alterado"

	if got := n.Calls()[0].Text; got != "a" {
		t.Errorf("alterar a cópia mudou o coletor: %q", got)
	}
}

func TestFailingRecordsTheCallAndReturnsTheError(t *testing.T) {
	boom := errors.New("boom")
	n := notifiertest.Failing(boom)

	err := n.Notify(context.Background(), 7, "oi")

	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
	if len(n.Calls()) != 1 {
		t.Errorf("a chamada que falha também deve ser registrada: %v", n.Calls())
	}
}

func TestOnNotifyRunsBeforeTheCallIsRecorded(t *testing.T) {
	n := notifiertest.New()
	var seenDuringHook int
	n.OnNotify(func(_ context.Context, chatID int64, text string) {
		seenDuringHook = len(n.Calls()) // não pode travar: o hook roda sem o mutex
		if chatID != 7 || text != "oi" {
			t.Errorf("hook recebeu (%d, %q)", chatID, text)
		}
	})

	_ = n.Notify(context.Background(), 7, "oi")

	if seenDuringHook != 0 {
		t.Errorf("no hook já havia %d chamada(s) registrada(s)", seenDuringHook)
	}
}

// Seguro para uso concorrente: rode com -race.
func TestNotifierIsSafeForConcurrentUse(t *testing.T) {
	n := notifiertest.New()
	const goroutines, each = 16, 50

	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				_ = n.Notify(context.Background(), 1, "x")
				_ = n.Calls()
			}
		}()
	}
	wg.Wait()

	if got := len(n.Calls()); got != goroutines*each {
		t.Errorf("chamadas = %d, want %d", got, goroutines*each)
	}
}
