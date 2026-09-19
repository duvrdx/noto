package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func debugLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

const waitLimit = 5 * time.Second // teto de segurança, nunca espera de verdade

func waitDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(waitLimit):
		t.Fatal("o laço do worker não retornou ao cancelamento do contexto")
		return nil
	}
}

// O laço retorna nil quando o contexto cai, mesmo sem nenhum tick: o tick é
// injetado como canal, então o teste não espera intervalo nenhum.
func TestWorkerLoopReturnsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tick := make(chan time.Time) // nunca dispara
	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- workerLoop(ctx, tick, debugLogger(&buf)) }()

	cancel()

	if err := waitDone(t, done); err != nil {
		t.Errorf("workerLoop devolveu %v, want nil", err)
	}
}

func TestWorkerLoopReturnsWhenContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var buf bytes.Buffer
	done := make(chan error, 1)

	go func() { done <- workerLoop(ctx, make(chan time.Time), debugLogger(&buf)) }()

	if err := waitDone(t, done); err != nil {
		t.Errorf("workerLoop devolveu %v, want nil", err)
	}
}

// Cada tick registra uma linha de log e nada mais: sem lógica de fila (M4).
func TestWorkerLoopLogsEachTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tick := make(chan time.Time)
	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- workerLoop(ctx, tick, debugLogger(&buf)) }()

	for range 3 {
		tick <- time.Time{}
	}
	cancel()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("workerLoop devolveu %v, want nil", err)
	}

	// lido só depois de done: happens-after de todas as escritas do laço
	if got := strings.Count(buf.String(), `"msg":"tick"`); got != 3 {
		t.Errorf("linhas de tick = %d, want 3\n%s", got, buf.String())
	}
}

// syncBuffer é um io.Writer seguro para ler enquanto o laço ainda escreve.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Com um ticker real de intervalo curto (2ms) o laço também tica e também
// sai; o teste espera o primeiro tick por polling, sem sleep fixo.
func TestWorkerLoopWithRealShortTicker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	var out syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- workerLoop(ctx, ticker.C, slog.New(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}()

	deadline := time.Now().Add(waitLimit)
	for !strings.Contains(out.String(), `"msg":"tick"`) {
		if time.Now().After(deadline) {
			t.Fatal("nenhum tick observado")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("workerLoop devolveu %v, want nil", err)
	}
}
