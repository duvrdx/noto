package main

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/duvrdx/noto/internal/platform/config"
	"github.com/duvrdx/noto/internal/testutil/testdb"
	"github.com/duvrdx/noto/internal/testutil/tgfake"
)

// Testes da subida do serve: falham antes de haver polling. Os que precisam de
// um banco alcançável usam o testdb (Docker); nenhum fala com a API real do
// Telegram.

const startupDBPass = "s3cr3t-dbpass"

// runServeCLI roda o serveIngest sob o despacho real (cli.run), com o stderr e o
// stdout capturados: o erro chega ao stderr só como mensagem.
func runServeCLI(t *testing.T, cfg config.Config, apiBaseURL string) (code int, stdout, stderr string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var out, errOut bytes.Buffer
	c := cli{
		stdout:     &out,
		stderr:     &errOut,
		loadConfig: func() (config.Config, error) { return cfg, nil },
		commands: map[string]command{"serve": func(ctx context.Context, cfg config.Config, log *slog.Logger) error {
			return serveIngest(ctx, cfg, ln, apiBaseURL, log)
		}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code = c.run(ctx, []string{"serve"})
	return code, out.String(), errOut.String()
}

func startupCfg(dsn string) config.Config {
	return config.Config{
		TelegramBotToken: e2eToken, TelegramTransport: config.TransportPolling,
		DatabaseURL: dsn, LogLevel: "debug", DefaultTimezone: "UTC",
	}
}

// O erro do NewClient chega ao stderr só como mensagem: com um token
// sentinela e falha forçada por URL base ruim (parâmetro injetado), nem o
// stderr nem o log têm o token. A fiação não extrai tipos da cadeia para exibir.
func TestServeStartupNewClientFailureNeverLeaksTheToken(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := "http://" + ln.Addr().String()
	ln.Close()
	for name, base := range map[string]string{
		"porta fechada":   closed,
		"URL malformada":  "http://[::1",
		"esquema ftp":     "ftp://127.0.0.1:1",
		"escape inválido": "http://exemplo.com/%zz",
	} {
		t.Run(name, func(t *testing.T) {
			pg := testdb.Pool(t).Config().ConnString()

			code, stdout, stderr := runServeCLI(t, startupCfg(pg), base)

			if code == 0 {
				t.Fatal("falha do NewClient deveria encerrar com código != 0")
			}
			for _, leak := range []string{e2eToken, e2eSecret} {
				if strings.Contains(stderr, leak) || strings.Contains(stdout, leak) {
					t.Errorf("o token vazou (%q):\nstdout: %s\nstderr: %s", leak, stdout, stderr)
				}
			}
		})
	}
}

// Token rejeitado (401 no getMe) derruba o serve com código != 0 e uma mensagem
// que diz isso, sem o token.
func TestServeStartupRejectedTokenExitsNonZeroWithoutLeakingIt(t *testing.T) {
	pg := testdb.Pool(t).Config().ConnString()
	fake := tgfake.New(t, e2eToken)
	fake.FailNext("getMe", tgfake.APIError(401, "Unauthorized"))

	code, stdout, stderr := runServeCLI(t, startupCfg(pg), fake.URL)

	if code == 0 {
		t.Fatal("token rejeitado deveria encerrar com código != 0")
	}
	if !strings.Contains(stderr, "token rejeitado") {
		t.Errorf("stderr deveria dizer \"token rejeitado\":\n%s", stderr)
	}
	for _, leak := range []string{e2eToken, e2eSecret} {
		if strings.Contains(stderr, leak) || strings.Contains(stdout, leak) {
			t.Errorf("o token vazou (%q):\nstdout: %s\nstderr: %s", leak, stdout, stderr)
		}
	}
}

// Ordem de subida: o banco vem antes do Telegram. Banco fora do ar derruba o
// serve sem nunca chamar o getMe, e sem a senha do DSN na saída.
func TestServeStartupDatabaseIsOpenedBeforeTelegram(t *testing.T) {
	fake := tgfake.New(t, e2eToken)
	dsn := "postgres://noto:" + startupDBPass + "@" + closedAddr(t) + "/noto?sslmode=disable"

	code, stdout, stderr := runServeCLI(t, startupCfg(dsn), fake.URL)

	if code == 0 {
		t.Fatal("banco fora do ar deveria encerrar com código != 0")
	}
	if got := fake.Attempts("getMe"); got != 0 {
		t.Errorf("o getMe foi chamado %d vez(es) com o banco fora do ar", got)
	}
	if !strings.Contains(stderr, "conect") {
		t.Errorf("stderr deveria trazer o erro de conexão:\n%s", stderr)
	}
	if strings.Contains(stderr, startupDBPass) || strings.Contains(stdout, startupDBPass) {
		t.Errorf("a senha do DSN vazou:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}
