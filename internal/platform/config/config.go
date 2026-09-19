package config

import (
	"errors"
	"fmt"
	"os"
	"time"
	_ "time/tzdata" // fuso IANA carregável mesmo em imagem sem tzdata do SO
)

// Valores aceitos para TELEGRAM_TRANSPORT (ADR 0008).
const (
	TransportPolling = "polling"
	TransportWebhook = "webhook"
)

// Config é a configuração completa do processo, lida do ambiente.
type Config struct {
	TelegramBotToken         string
	TelegramTransport        string
	TelegramWebhookSecret    string
	OllamaHost               string
	OllamaAPIKey             string
	OllamaModel              string
	DatabaseURL              string
	LogLevel                 string
	DefaultTimezone          string
	OTelExporterOTLPEndpoint string
}

// Load lê o ambiente, aplica defaults e valida. Em caso de problema devolve
// um único erro que cita todas as variáveis reprovadas, não só a primeira.
//
// No scaffolding só DATABASE_URL é incondicionalmente obrigatória;
// TELEGRAM_WEBHOOK_SECRET é obrigatória apenas com TELEGRAM_TRANSPORT=webhook.
// Variável definida como string vazia equivale a não definida.
func Load() (Config, error) {
	c := Config{
		TelegramBotToken:         os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramTransport:        getenv("TELEGRAM_TRANSPORT", TransportPolling),
		TelegramWebhookSecret:    os.Getenv("TELEGRAM_WEBHOOK_SECRET"),
		OllamaHost:               getenv("OLLAMA_HOST", "https://ollama.com"),
		OllamaAPIKey:             os.Getenv("OLLAMA_API_KEY"),
		OllamaModel:              os.Getenv("OLLAMA_MODEL"),
		DatabaseURL:              os.Getenv("DATABASE_URL"),
		LogLevel:                 getenv("LOG_LEVEL", "info"),
		DefaultTimezone:          getenv("DEFAULT_TIMEZONE", "America/Sao_Paulo"),
		OTelExporterOTLPEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	}

	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL: obrigatória e não definida"))
	}
	switch c.TelegramTransport {
	case TransportPolling:
	case TransportWebhook:
		if c.TelegramWebhookSecret == "" {
			errs = append(errs, errors.New("TELEGRAM_WEBHOOK_SECRET: obrigatória quando TELEGRAM_TRANSPORT=webhook"))
		}
	default:
		errs = append(errs, fmt.Errorf("TELEGRAM_TRANSPORT: valor %q inválido, aceitos: %s, %s",
			c.TelegramTransport, TransportPolling, TransportWebhook))
	}
	if _, err := time.LoadLocation(c.DefaultTimezone); err != nil {
		errs = append(errs, fmt.Errorf("DEFAULT_TIMEZONE: %q não é um fuso IANA carregável", c.DefaultTimezone))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("configuração inválida: %w", errors.Join(errs...))
	}
	return c, nil
}

// getenv devolve a variável, ou def quando ela está ausente ou vazia.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
