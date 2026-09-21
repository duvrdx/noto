// Package tgfake é uma API do Telegram falsa, sobre httptest, para os testes
// de contrato do adapter: emula getMe, getUpdates e sendMessage. É código de
// teste, nenhum pacote de produção o importa, e nenhum teste com ele toca a
// rede real.
//
// O servidor exige o token no caminho (/bot<token>/<método>): outro token
// recebe 401, como a API real. Entrega os lotes de updates que o teste
// enfileira, um por consulta a getUpdates; sem lote, segura a consulta por
// pouco tempo e devolve [] (sem isso o laço do cliente giraria sem espera).
// Respostas de erro, JSON malformado e queda de conexão são roteirizados por
// método com FailNext. Grava o que o teste precisa afirmar (offsets,
// allowed_updates, mensagens enviadas) e nada além: o fake nunca guarda o
// corpo de um update.
package tgfake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// holdTime é quanto um getUpdates sem lote espera antes de devolver [].
const holdTime = 50 * time.Millisecond

// Sent é uma chamada a sendMessage, como o fake a recebeu.
type Sent struct {
	ChatID       int64
	Text         string
	ParseMode    string
	HasParseMode bool // o campo parse_mode veio no formulário, mesmo vazio
}

// Reply é uma resposta roteirizada no lugar da resposta normal de um método.
type Reply struct {
	status int
	body   string
	hangup bool
}

// APIError responde como a API do Telegram: HTTP code e o envelope de erro.
func APIError(code int, description string) Reply {
	b, _ := json.Marshal(map[string]any{"ok": false, "error_code": code, "description": description})
	return Reply{status: code, body: string(b)}
}

// RetryAfter responde 429 com parameters.retry_after.
func RetryAfter(seconds int) Reply {
	b, _ := json.Marshal(map[string]any{
		"ok": false, "error_code": 429, "description": "Too Many Requests: retry after " + strconv.Itoa(seconds),
		"parameters": map[string]any{"retry_after": seconds},
	})
	return Reply{status: http.StatusTooManyRequests, body: string(b)}
}

// Malformed responde 200 com um corpo que não é o envelope JSON esperado.
func Malformed(body string) Reply { return Reply{status: http.StatusOK, body: body} }

// Hangup derruba a conexão sem responder.
func Hangup() Reply { return Reply{hangup: true} }

// Server é a API falsa.
type Server struct {
	// URL é a base a passar ao cliente (sem /bot<token>).
	URL string

	token  string
	srv    *httptest.Server
	closed chan struct{}
	wake   chan struct{}

	mu             sync.Mutex
	batches        [][]json.RawMessage
	replies        map[string][]Reply
	offsets        []int64
	allowed        []string
	sent           []Sent
	getUpdatesHits int
	nextMessageID  int
}

// New sobe o servidor para o token dado e o derruba no t.Cleanup.
func New(t testing.TB, token string) *Server {
	t.Helper()
	s := &Server{
		token:   token,
		closed:  make(chan struct{}),
		wake:    make(chan struct{}, 1),
		replies: map[string][]Reply{},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	s.URL = s.srv.URL
	t.Cleanup(s.Close)
	return s
}

// Close encerra o servidor; consultas seguradas são liberadas antes.
func (s *Server) Close() {
	select {
	case <-s.closed:
		return
	default:
		close(s.closed)
	}
	s.srv.CloseClientConnections()
	s.srv.Close()
}

// Enqueue enfileira um lote: a próxima consulta a getUpdates o recebe inteiro.
func (s *Server) Enqueue(updates ...json.RawMessage) {
	s.mu.Lock()
	s.batches = append(s.batches, updates)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// FailNext faz a próxima chamada ao método (ex.: "getUpdates") receber r no
// lugar da resposta normal. Várias chamadas enfileiram várias respostas.
func (s *Server) FailNext(method string, r Reply) {
	s.mu.Lock()
	s.replies[method] = append(s.replies[method], r)
	s.mu.Unlock()
}

// Offsets devolve o offset de cada consulta a getUpdates, em ordem (0 quando
// o cliente não o enviou).
func (s *Server) Offsets() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.offsets...)
}

// AllowedUpdates devolve o valor cru de allowed_updates de cada consulta a
// getUpdates (vazio quando ausente).
func (s *Server) AllowedUpdates() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.allowed...)
}

// Sent devolve as chamadas a sendMessage, em ordem.
func (s *Server) Sent() []Sent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Sent(nil), s.sent...)
}

// GetUpdatesCalls devolve quantas consultas a getUpdates chegaram.
func (s *Server) GetUpdatesCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getUpdatesHits
}

// Eventually espera cond ficar verdadeira até o prazo, sem sleep fixo na
// asserção; falha o teste com msg se estourar.
func Eventually(t testing.TB, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("prazo de %v estourou: %s", timeout, msg)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// WaitOffset espera uma consulta a getUpdates com offset >= min.
func (s *Server) WaitOffset(t testing.TB, min int64, timeout time.Duration) {
	t.Helper()
	Eventually(t, timeout, func() bool {
		for _, o := range s.Offsets() {
			if o >= min {
				return true
			}
		}
		return false
	}, fmt.Sprintf("nenhuma consulta com offset >= %d (offsets: %v)", min, s.Offsets()))
}

// WaitSent espera pelo menos n chamadas a sendMessage.
func (s *Server) WaitSent(t testing.TB, n int, timeout time.Duration) {
	t.Helper()
	Eventually(t, timeout, func() bool { return len(s.Sent()) >= n }, fmt.Sprintf("menos de %d sendMessage recebidos", n))
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/bot")
	if !ok {
		writeJSON(w, http.StatusNotFound, `{"ok":false,"error_code":404,"description":"Not Found"}`)
		return
	}
	token, method, ok := strings.Cut(rest, "/")
	if !ok {
		writeJSON(w, http.StatusNotFound, `{"ok":false,"error_code":404,"description":"Not Found"}`)
		return
	}
	if token != s.token {
		writeJSON(w, http.StatusUnauthorized, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
		return
	}
	// O cliente manda multipart/form-data; getMe vai sem corpo.
	if err := r.ParseMultipartForm(1 << 20); err != nil && err != http.ErrNotMultipart {
		writeJSON(w, http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"Bad Request: bad form"}`)
		return
	}

	if reply, ok := s.popReply(method); ok {
		if reply.hangup {
			s.hangup(w)
			return
		}
		writeJSON(w, reply.status, reply.body)
		return
	}

	switch method {
	case "getMe":
		writeJSON(w, http.StatusOK, `{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"Fake","username":"fake_bot"}}`)
	case "getUpdates":
		s.getUpdates(w, r)
	case "sendMessage":
		s.sendMessage(w, r)
	default:
		writeJSON(w, http.StatusNotFound, `{"ok":false,"error_code":404,"description":"Not Found"}`)
	}
}

func (s *Server) popReply(method string) (Reply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.replies[method]
	if len(q) == 0 {
		return Reply{}, false
	}
	s.replies[method] = q[1:]
	return q[0], true
}

func (s *Server) hangup(w http.ResponseWriter) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func (s *Server) getUpdates(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.ParseInt(r.FormValue("offset"), 10, 64)
	s.mu.Lock()
	s.getUpdatesHits++
	s.offsets = append(s.offsets, offset)
	s.allowed = append(s.allowed, r.FormValue("allowed_updates"))
	s.mu.Unlock()

	batch, ok := s.popBatch()
	if !ok {
		// Sem lote: segura a consulta até chegar um, o prazo acabar, o cliente
		// desistir ou o servidor fechar; nunca gira sem espera.
		timer := time.NewTimer(holdTime)
		defer timer.Stop()
		select {
		case <-s.wake:
			batch, _ = s.popBatch()
		case <-timer.C:
		case <-r.Context().Done():
			return
		case <-s.closed:
			return
		}
	}
	if batch == nil {
		batch = []json.RawMessage{}
	}
	b, _ := json.Marshal(map[string]any{"ok": true, "result": batch})
	writeJSON(w, http.StatusOK, string(b))
}

func (s *Server) popBatch() ([]json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.batches) == 0 {
		return nil, false
	}
	b := s.batches[0]
	s.batches = s.batches[1:]
	return b, true
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	chatID, _ := strconv.ParseInt(r.FormValue("chat_id"), 10, 64)
	_, hasMode := r.MultipartForm.Value["parse_mode"]
	sent := Sent{ChatID: chatID, Text: r.FormValue("text"), ParseMode: r.FormValue("parse_mode"), HasParseMode: hasMode}

	s.mu.Lock()
	s.sent = append(s.sent, sent)
	s.nextMessageID++
	id := s.nextMessageID
	s.mu.Unlock()

	b, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{
		"message_id": id, "date": 1, "chat": map[string]any{"id": chatID, "type": "private"}, "text": sent.Text,
	}})
	writeJSON(w, http.StatusOK, string(b))
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// --- Construtores de update ---------------------------------------------------

func marshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// TextUpdate é uma mensagem de texto em chat privado.
func TextUpdate(updateID, chatID, userID int64, text string) json.RawMessage {
	return chatTextUpdate(updateID, chatID, userID, "private", text)
}

// ChatTextUpdate é uma mensagem de texto em chat do tipo dado (group, supergroup, channel...).
func ChatTextUpdate(updateID, chatID, userID int64, chatType, text string) json.RawMessage {
	return chatTextUpdate(updateID, chatID, userID, chatType, text)
}

func chatTextUpdate(updateID, chatID, userID int64, chatType, text string) json.RawMessage {
	return marshal(map[string]any{"update_id": updateID, "message": map[string]any{
		"message_id": updateID, "date": 1,
		"from": map[string]any{"id": userID, "is_bot": false, "first_name": "Fulano"},
		"chat": map[string]any{"id": chatID, "type": chatType},
		"text": text,
	}})
}

// NoFromUpdate é uma mensagem de texto privada sem o campo from.
func NoFromUpdate(updateID, chatID int64, text string) json.RawMessage {
	return marshal(map[string]any{"update_id": updateID, "message": map[string]any{
		"message_id": updateID, "date": 1,
		"chat": map[string]any{"id": chatID, "type": "private"},
		"text": text,
	}})
}

// PhotoUpdate é uma mensagem privada com foto e sem text.
func PhotoUpdate(updateID, chatID, userID int64) json.RawMessage {
	return marshal(map[string]any{"update_id": updateID, "message": map[string]any{
		"message_id": updateID, "date": 1,
		"from":  map[string]any{"id": userID, "is_bot": false, "first_name": "Fulano"},
		"chat":  map[string]any{"id": chatID, "type": "private"},
		"photo": []map[string]any{{"file_id": "f", "file_unique_id": "u", "width": 1, "height": 1}},
	}})
}

// RawUpdate devolve o JSON literal, para updates malformados.
func RawUpdate(s string) json.RawMessage { return json.RawMessage(s) }
