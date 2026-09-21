package tgfake_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/duvrdx/noto/internal/testutil/tgfake"
)

const token = "123456:SENTINELA-tgfake"

// call faz uma chamada como o cliente da biblioteca: POST multipart em
// /bot<token>/<método>.
func call(t *testing.T, baseURL, tok, method string, fields map[string]string) (int, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	resp, err := http.Post(baseURL+"/bot"+tok+"/"+method, w.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestWrongTokenIsUnauthorized(t *testing.T) {
	s := tgfake.New(t, token)

	code, _ := call(t, s.URL, "999:outro", "getMe", nil)

	if code != http.StatusUnauthorized {
		t.Errorf("token errado: status %d, want 401", code)
	}
}

func TestUnknownMethodIsNotFound(t *testing.T) {
	s := tgfake.New(t, token)

	if code, _ := call(t, s.URL, token, "deleteEverything", nil); code != http.StatusNotFound {
		t.Errorf("status %d, want 404", code)
	}
}

func TestGetMeAnswers(t *testing.T) {
	s := tgfake.New(t, token)

	code, body := call(t, s.URL, token, "getMe", nil)

	if code != 200 || !strings.Contains(body, `"is_bot":true`) {
		t.Errorf("getMe = %d %s", code, body)
	}
}

func TestGetUpdatesDeliversBatchAndRecordsOffsetAndAllowedUpdates(t *testing.T) {
	s := tgfake.New(t, token)
	s.Enqueue(tgfake.TextUpdate(10, 7, 42, "oi"), tgfake.TextUpdate(11, 7, 42, "de novo"))

	code, body := call(t, s.URL, token, "getUpdates", map[string]string{"offset": "10", "timeout": "1", "allowed_updates": `["message"]`})

	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	var env struct {
		Result []struct {
			UpdateID int64 `json:"update_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Result) != 2 || env.Result[0].UpdateID != 10 || env.Result[1].UpdateID != 11 {
		t.Errorf("lote = %+v", env.Result)
	}
	if got := s.Offsets(); len(got) != 1 || got[0] != 10 {
		t.Errorf("Offsets() = %v, want [10]", got)
	}
	if got := s.AllowedUpdates(); len(got) != 1 || got[0] != `["message"]` {
		t.Errorf("AllowedUpdates() = %v", got)
	}
}

// Sem lote, a consulta é segurada e devolve [], e o fake não vira busy-loop:
// um cliente que insiste durante 300 ms faz poucas consultas, não milhares.
func TestGetUpdatesWithoutBatchHoldsAndReturnsEmpty(t *testing.T) {
	s := tgfake.New(t, token)
	begin := time.Now()

	code, body := call(t, s.URL, token, "getUpdates", map[string]string{"offset": "1", "timeout": "1"})

	if code != 200 || !strings.Contains(body, `"result":[]`) {
		t.Errorf("getUpdates vazio = %d %s", code, body)
	}
	if took := time.Since(begin); took < 30*time.Millisecond {
		t.Errorf("respondeu em %v; deveria segurar a consulta (~50 ms)", took)
	}
}

func TestSendMessageRecordsFieldsAndAbsenceOfParseMode(t *testing.T) {
	s := tgfake.New(t, token)

	call(t, s.URL, token, "sendMessage", map[string]string{"chat_id": "7", "text": "*negrito* <b>x</b>"})
	call(t, s.URL, token, "sendMessage", map[string]string{"chat_id": "8", "text": "y", "parse_mode": "HTML"})

	got := s.Sent()
	if len(got) != 2 {
		t.Fatalf("Sent() = %v", got)
	}
	if got[0].ChatID != 7 || got[0].Text != "*negrito* <b>x</b>" || got[0].HasParseMode {
		t.Errorf("primeiro = %+v", got[0])
	}
	if !got[1].HasParseMode || got[1].ParseMode != "HTML" {
		t.Errorf("segundo = %+v", got[1])
	}
}

func TestFailNextScriptsErrorsPerMethodAndOnlyOnce(t *testing.T) {
	s := tgfake.New(t, token)
	s.FailNext("getMe", tgfake.APIError(401, "Unauthorized"))
	s.FailNext("sendMessage", tgfake.APIError(403, "Forbidden: bot was blocked by the user"))
	s.FailNext("getUpdates", tgfake.RetryAfter(3))
	s.FailNext("getUpdates", tgfake.Malformed("isto não é JSON"))

	if code, _ := call(t, s.URL, token, "getMe", nil); code != 401 {
		t.Errorf("getMe roteirizado: %d, want 401", code)
	}
	if code, _ := call(t, s.URL, token, "getMe", nil); code != 200 {
		t.Errorf("a segunda getMe deveria ser normal: %d", code)
	}
	if code, _ := call(t, s.URL, token, "sendMessage", map[string]string{"chat_id": "7", "text": "x"}); code != 403 {
		t.Errorf("sendMessage roteirizado: %d, want 403", code)
	}
	if code, body := call(t, s.URL, token, "getUpdates", nil); code != 429 || !strings.Contains(body, `"retry_after":3`) {
		t.Errorf("getUpdates 429: %d %s", code, body)
	}
	if code, body := call(t, s.URL, token, "getUpdates", nil); code != 200 || body != "isto não é JSON" {
		t.Errorf("getUpdates malformado: %d %q", code, body)
	}
	if n := len(s.Sent()); n != 0 {
		t.Errorf("a chamada que falhou não deveria ser gravada como enviada: %d", n)
	}
}

func TestHangupDropsTheConnection(t *testing.T) {
	s := tgfake.New(t, token)
	s.FailNext("getMe", tgfake.Hangup())

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.Close()
	_, err := http.Post(s.URL+"/bot"+token+"/getMe", w.FormDataContentType(), &body)

	if err == nil {
		t.Error("esperava erro de conexão")
	}
}

// O fake não vaza goroutines: nem as consultas seguradas, nem o servidor.
func TestCloseReleasesHeldRequestsAndLeaksNoGoroutines(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()

	s := tgfake.New(t, token)
	done := make(chan struct{})
	go func() {
		defer close(done)
		client := &http.Client{Timeout: 5 * time.Second}
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		w.Close()
		resp, err := client.Post(s.URL+"/bot"+token+"/getUpdates", w.FormDataContentType(), &body)
		if err == nil {
			resp.Body.Close()
		}
	}()
	tgfake.Eventually(t, 2*time.Second, func() bool { return s.GetUpdatesCalls() == 1 }, "a consulta não chegou ao fake")

	s.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close não liberou a consulta segurada")
	}
	http.DefaultTransport.(*http.Transport).CloseIdleConnections()
	tgfake.Eventually(t, 3*time.Second, func() bool { return runtime.NumGoroutine() <= before }, "goroutines vazaram depois do Close")
}
