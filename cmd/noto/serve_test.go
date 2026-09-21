package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/duvrdx/noto/internal/platform/config"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestHealthzRespondsOK(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	newServeMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", rec.Code)
	}
}

func TestHealthzRejectsOtherMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)

	newServeMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz = %d, want 405", rec.Code)
	}
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()

	newServeMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nada", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /nada = %d, want 404", rec.Code)
	}
}

// serve atende de verdade num listener real e encerra de forma graciosa
// quando o contexto é cancelado: devolve nil e para de aceitar conexões.
func TestServeShutsDownGracefullyOnContextCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, ln, discardLogger()) }()

	url := "http://" + ln.Addr().String() + "/healthz"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve devolveu %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve não encerrou após o cancelamento do contexto")
	}

	client := http.Client{Timeout: time.Second}
	if resp, err := client.Get(url); err == nil {
		resp.Body.Close()
		t.Error("servidor ainda aceita conexões após o shutdown")
	}
}

// serve não depende do worker: só precisa do listener.
func TestServeFailsWhenListenerIsClosed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln.Close()

	if err := serve(context.Background(), ln, discardLogger()); err == nil {
		t.Error("serve com listener fechado deveria devolver erro")
	}
}

// --- runServe: webhook recusado antes de qualquer E/S --------------------------

// closedAddr (migrate_test.go) devolve host:porta de loopback recusando conexão.

// Com TELEGRAM_TRANSPORT=webhook o serve recusa na subida, citando a variável
// e o polling, antes de abrir o banco e antes de qualquer chamada de rede: o
// DATABASE_URL aponta para uma porta fechada e o erro NÃO é de conexão.
func TestServeRefusesWebhookBeforeAnyIO(t *testing.T) {
	const (
		token  = "123456:SENTINELA-webhook-token"
		secret = "sentinela-webhook-secret"
		dbPass = "s3cr3t-dbpass"
	)
	t.Setenv("TELEGRAM_BOT_TOKEN", token)
	t.Setenv("TELEGRAM_TRANSPORT", "webhook")
	t.Setenv("TELEGRAM_WEBHOOK_SECRET", secret)
	t.Setenv("DATABASE_URL", "postgres://noto:"+dbPass+"@"+closedAddr(t)+"/noto?sslmode=disable")
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	begin := time.Now()

	code := defaultCLI(&stdout, &stderr).run(ctx, []string{"serve"})

	if code == 0 {
		t.Fatal("código = 0, want != 0")
	}
	if took := time.Since(begin); took > 3*time.Second {
		t.Errorf("a recusa demorou %v: parece ter feito E/S", took)
	}
	msg := stderr.String()
	for _, want := range []string{"TELEGRAM_TRANSPORT", "polling"} {
		if !strings.Contains(msg, want) {
			t.Errorf("stderr deveria citar %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "conect") || strings.Contains(msg, "banco") {
		t.Errorf("o erro parece de conexão com o banco, e não de recusa do webhook:\n%s", msg)
	}
	// A config redigida foi logada na subida, sem nenhum segredo.
	if !strings.Contains(stdout.String(), `"iniciando"`) || !strings.Contains(stdout.String(), "[REDACTED]") {
		t.Errorf("faltou o log de subida com a config redigida:\n%s", stdout.String())
	}
	for _, s := range []string{token, "SENTINELA-webhook-token", secret, dbPass} {
		if strings.Contains(stdout.String(), s) || strings.Contains(msg, s) {
			t.Errorf("segredo %q apareceu em stdout ou stderr:\nstdout: %s\nstderr: %s", s, stdout.String(), msg)
		}
	}
}

// runServe direto (sem passar pelo despacho): a recusa é do próprio comando.
func TestRunServeWebhookErrorNamesVariableAndPolling(t *testing.T) {
	cfg := config.Config{TelegramTransport: config.TransportWebhook, TelegramBotToken: "tok", DatabaseURL: "postgres://x@" + closedAddr(t) + "/noto"}

	err := runServe(context.Background(), cfg, discardLogger())

	if err == nil {
		t.Fatal("webhook deveria ser recusado")
	}
	for _, want := range []string{"TELEGRAM_TRANSPORT", "polling"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("erro %q deveria citar %q", err, want)
		}
	}
}

// Nenhuma rota de webhook é registrada.
func TestNoWebhookRouteIsRegistered(t *testing.T) {
	for _, path := range []string{"/webhook", "/telegram/webhook", "/telegram"} {
		rec := httptest.NewRecorder()
		newServeMux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, rec.Code)
		}
	}
}
