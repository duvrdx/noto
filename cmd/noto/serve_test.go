package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
