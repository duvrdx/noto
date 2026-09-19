package config_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/duvrdx/noto/internal/platform/config"
)

// knownVars é o conjunto de variáveis reconhecidas (spec configuration).
var knownVars = []string{
	"TELEGRAM_BOT_TOKEN",
	"TELEGRAM_TRANSPORT",
	"TELEGRAM_WEBHOOK_SECRET",
	"OLLAMA_HOST",
	"OLLAMA_API_KEY",
	"OLLAMA_MODEL",
	"DATABASE_URL",
	"LOG_LEVEL",
	"DEFAULT_TIMEZONE",
	"OTEL_EXPORTER_OTLP_ENDPOINT",
}

// setEnv deixa o ambiente do teste exatamente como em env: toda variável
// conhecida fora do mapa é removida (não apenas esvaziada), e as do mapa
// recebem o valor literal, inclusive "". Os valores originais são
// restaurados ao fim do teste. Nenhum teste lê o arquivo .env.example.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range knownVars {
		t.Setenv(k, "") // registra a restauração do valor original
		if v, ok := env[k]; ok {
			t.Setenv(k, v)
			continue
		}
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unsetenv %s: %v", k, err)
		}
	}
	// variáveis fora do conjunto conhecido são apenas definidas
	for k, v := range env {
		if !slices.Contains(knownVars, k) {
			t.Setenv(k, v)
		}
	}
}

const validDB = "postgres://noto:noto@localhost:5432/noto?sslmode=disable"

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr []string // nomes de variáveis que a mensagem de erro deve citar; vazio = sucesso
		check   func(t *testing.T, c config.Config)
	}{
		{
			name: "carga completa",
			env: map[string]string{
				"TELEGRAM_BOT_TOKEN":          "tok",
				"TELEGRAM_TRANSPORT":          "webhook",
				"TELEGRAM_WEBHOOK_SECRET":     "sec",
				"OLLAMA_HOST":                 "http://localhost:11434",
				"OLLAMA_API_KEY":              "key",
				"OLLAMA_MODEL":                "algum-modelo",
				"DATABASE_URL":                validDB,
				"LOG_LEVEL":                   "debug",
				"DEFAULT_TIMEZONE":            "UTC",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4317",
			},
			check: func(t *testing.T, c config.Config) {
				want := config.Config{
					TelegramBotToken:         "tok",
					TelegramTransport:        "webhook",
					TelegramWebhookSecret:    "sec",
					OllamaHost:               "http://localhost:11434",
					OllamaAPIKey:             "key",
					OllamaModel:              "algum-modelo",
					DatabaseURL:              validDB,
					LogLevel:                 "debug",
					DefaultTimezone:          "UTC",
					OTelExporterOTLPEndpoint: "http://localhost:4317",
				}
				if c != want {
					t.Errorf("config = %+v, want %+v", c, want)
				}
			},
		},
		{
			name: "apenas DATABASE_URL: defaults aplicados, opcionais vazios",
			env:  map[string]string{"DATABASE_URL": validDB},
			check: func(t *testing.T, c config.Config) {
				if c.LogLevel != "info" {
					t.Errorf("LogLevel = %q, want info", c.LogLevel)
				}
				if c.TelegramTransport != "polling" {
					t.Errorf("TelegramTransport = %q, want polling", c.TelegramTransport)
				}
				if c.DefaultTimezone != "America/Sao_Paulo" {
					t.Errorf("DefaultTimezone = %q, want America/Sao_Paulo", c.DefaultTimezone)
				}
				if c.OllamaHost != "https://ollama.com" {
					t.Errorf("OllamaHost = %q, want https://ollama.com", c.OllamaHost)
				}
				if c.TelegramBotToken != "" || c.OllamaAPIKey != "" || c.OllamaModel != "" || c.TelegramWebhookSecret != "" {
					t.Errorf("opcionais deveriam ser vazios: %+v", c)
				}
			},
		},
		{
			// O caso mais importante: um .env copiado do exemplo tem estes
			// campos vazios (definidos, mas ""), e a carga precisa passar.
			name: ".env.example recém-copiado com opcionais vazios",
			env: map[string]string{
				"TELEGRAM_BOT_TOKEN":          "",
				"TELEGRAM_TRANSPORT":          "polling",
				"TELEGRAM_WEBHOOK_SECRET":     "",
				"OLLAMA_HOST":                 "https://ollama.com",
				"OLLAMA_API_KEY":              "",
				"OLLAMA_MODEL":                "",
				"DATABASE_URL":                validDB,
				"LOG_LEVEL":                   "info",
				"DEFAULT_TIMEZONE":            "America/Sao_Paulo",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4317",
			},
			check: func(t *testing.T, c config.Config) {
				if c.TelegramTransport != "polling" || c.DatabaseURL != validDB {
					t.Errorf("config inesperada: %+v", c)
				}
				if c.TelegramBotToken != "" || c.TelegramWebhookSecret != "" || c.OllamaAPIKey != "" || c.OllamaModel != "" {
					t.Errorf("opcionais deveriam ser vazios: %+v", c)
				}
			},
		},
		{
			name: "variável fora do conjunto é ignorada",
			env:  map[string]string{"DATABASE_URL": validDB, "NOTO_VARIAVEL_DESCONHECIDA": "x"},
		},
		{
			name:    "DATABASE_URL ausente",
			env:     map[string]string{},
			wantErr: []string{"DATABASE_URL"},
		},
		{
			name:    "DATABASE_URL vazia",
			env:     map[string]string{"DATABASE_URL": ""},
			wantErr: []string{"DATABASE_URL"},
		},
		{
			name:    "TELEGRAM_TRANSPORT inválido",
			env:     map[string]string{"DATABASE_URL": validDB, "TELEGRAM_TRANSPORT": "carrier-pigeon"},
			wantErr: []string{"TELEGRAM_TRANSPORT", "polling", "webhook"},
		},
		{
			name:    "webhook sem TELEGRAM_WEBHOOK_SECRET",
			env:     map[string]string{"DATABASE_URL": validDB, "TELEGRAM_TRANSPORT": "webhook"},
			wantErr: []string{"TELEGRAM_WEBHOOK_SECRET"},
		},
		{
			name: "webhook com TELEGRAM_WEBHOOK_SECRET",
			env:  map[string]string{"DATABASE_URL": validDB, "TELEGRAM_TRANSPORT": "webhook", "TELEGRAM_WEBHOOK_SECRET": "sec"},
		},
		{
			name:    "DEFAULT_TIMEZONE inválido",
			env:     map[string]string{"DATABASE_URL": validDB, "DEFAULT_TIMEZONE": "Mars/Olympus_Mons"},
			wantErr: []string{"DEFAULT_TIMEZONE"},
		},
		{
			name: "múltiplos erros agregados numa mensagem",
			env: map[string]string{
				"TELEGRAM_TRANSPORT": "carrier-pigeon",
				"DEFAULT_TIMEZONE":   "Mars/Olympus_Mons",
			},
			wantErr: []string{"DATABASE_URL", "TELEGRAM_TRANSPORT", "DEFAULT_TIMEZONE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.env)

			c, err := config.Load()

			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("Load() sem erro, queria citar %v", tt.wantErr)
				}
				for _, name := range tt.wantErr {
					if !strings.Contains(err.Error(), name) {
						t.Errorf("erro %q não cita %q", err.Error(), name)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() erro inesperado: %v", err)
			}
			if tt.check != nil {
				tt.check(t, c)
			}
		})
	}
}

// Quando há vários problemas, tudo vem numa única mensagem — não só o primeiro.
func TestLoadAggregatesErrorsInSingleMessage(t *testing.T) {
	setEnv(t, map[string]string{"TELEGRAM_TRANSPORT": "carrier-pigeon"})

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() sem erro")
	}
	msg := err.Error()
	if !strings.Contains(msg, "DATABASE_URL") || !strings.Contains(msg, "TELEGRAM_TRANSPORT") {
		t.Errorf("mensagem deveria citar as duas variáveis: %q", msg)
	}
}

const (
	secretToken   = "s3cr3t-token"
	secretWebhook = "s3cr3t-webhook"
	secretAPIKey  = "s3cr3t-apikey"
	secretDBPass  = "s3cr3t-dbpass"
)

func sensitiveConfig() config.Config {
	return config.Config{
		TelegramBotToken:      secretToken,
		TelegramTransport:     "webhook",
		TelegramWebhookSecret: secretWebhook,
		OllamaHost:            "https://ollama.com",
		OllamaAPIKey:          secretAPIKey,
		OllamaModel:           "algum-modelo",
		DatabaseURL:           "postgres://noto:" + secretDBPass + "@db.internal:5432/noto?sslmode=disable",
		LogLevel:              "info",
		DefaultTimezone:       "America/Sao_Paulo",
	}
}

// renderings devolve a config em todas as formas de saída relevantes.
func renderings(t *testing.T, c config.Config) map[string]string {
	t.Helper()
	out := map[string]string{
		"%v":           fmt.Sprintf("%v", c),
		"%+v":          fmt.Sprintf("%+v", c),
		"%#v":          fmt.Sprintf("%#v", c),
		"%v ponteiro":  fmt.Sprintf("%v", &c),
		"%+v ponteiro": fmt.Sprintf("%+v", &c),
	}
	//lint:ignore S1025 o verbo %s é o objeto do teste; .String() não o exercitaria
	out["%s"] = fmt.Sprintf("%s", c)
	for name, mk := range map[string]func(*bytes.Buffer) *slog.Logger{
		"slog JSON":  func(b *bytes.Buffer) *slog.Logger { return slog.New(slog.NewJSONHandler(b, nil)) },
		"slog texto": func(b *bytes.Buffer) *slog.Logger { return slog.New(slog.NewTextHandler(b, nil)) },
	} {
		var b bytes.Buffer
		mk(&b).Info("config carregada", "config", c)
		out[name] = b.String()
		b.Reset()
		mk(&b).Info("config carregada", "config", &c)
		out[name+" ponteiro"] = b.String()
	}
	return out
}

func TestConfigRedactsSecretsInEveryOutput(t *testing.T) {
	for form, got := range renderings(t, sensitiveConfig()) {
		t.Run(form, func(t *testing.T) {
			for _, secret := range []string{secretToken, secretWebhook, secretAPIKey, secretDBPass} {
				if strings.Contains(got, secret) {
					t.Errorf("saída vaza %q: %s", secret, got)
				}
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Errorf("saída sem marcador de redação: %s", got)
			}
		})
	}
}

func TestConfigLogKeepsDiagnosticFields(t *testing.T) {
	var b bytes.Buffer
	slog.New(slog.NewJSONHandler(&b, nil)).Info("config carregada", "config", sensitiveConfig())
	got := b.String()
	for _, want := range []string{"db.internal", "5432", "/noto", "noto", "webhook", "https://ollama.com", "algum-modelo", "America/Sao_Paulo"} {
		if !strings.Contains(got, want) {
			t.Errorf("saída deveria conservar %q: %s", want, got)
		}
	}
}

func TestConfigRedactionOfDatabaseURLEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		url  string
		leak string   // não pode aparecer
		keep []string // precisa aparecer
	}{
		{"sem senha", "postgres://noto@localhost:5432/noto", "", []string{"localhost:5432/noto"}},
		{"sem credenciais", "postgres://localhost/noto", "", []string{"localhost/noto"}},
		{"senha na query", "postgres://localhost/noto?sslmode=disable&password=" + secretDBPass, secretDBPass, []string{"localhost/noto"}},
		{"malformada: escape inválido", "postgres://noto:" + secretDBPass + "%zz@localhost/noto", secretDBPass, nil},
		{"malformada: porta inválida", "postgres://noto:" + secretDBPass + "/x@localhost/noto", secretDBPass, nil},
		{"formato chave=valor", "host=localhost dbname=noto password=" + secretDBPass, secretDBPass, nil},
		{"vazia", "", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := sensitiveConfig()
			c.DatabaseURL = tt.url
			for form, got := range renderings(t, c) {
				if tt.leak != "" && strings.Contains(got, tt.leak) {
					t.Errorf("%s vaza %q: %s", form, tt.leak, got)
				}
			}
			var b bytes.Buffer
			slog.New(slog.NewJSONHandler(&b, nil)).Info("x", "config", c)
			for _, k := range tt.keep {
				if !strings.Contains(b.String(), k) {
					t.Errorf("deveria conservar %q: %s", k, b.String())
				}
			}
		})
	}
}
