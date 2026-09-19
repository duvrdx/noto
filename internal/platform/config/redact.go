package config

import (
	"log/slog"
	"net/url"
	"strings"
)

// Redacted é o marcador que substitui valores sensíveis na saída.
const Redacted = "[REDACTED]"

// LogValue implementa slog.LogValuer: quem logar a Config, de onde for,
// nunca vaza segredos (PRD §12). Token do Telegram, segredo do webhook e
// chave do Ollama saem sempre como Redacted; de DATABASE_URL só a senha é
// removida, porque host e database são informação de diagnóstico legítima.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("telegram_bot_token", Redacted),
		slog.String("telegram_transport", c.TelegramTransport),
		slog.String("telegram_webhook_secret", Redacted),
		slog.String("ollama_host", c.OllamaHost),
		slog.String("ollama_api_key", Redacted),
		slog.String("ollama_model", c.OllamaModel),
		slog.String("database_url", redactDatabaseURL(c.DatabaseURL)),
		slog.String("log_level", c.LogLevel),
		slog.String("default_timezone", c.DefaultTimezone),
		slog.String("otel_exporter_otlp_endpoint", c.OTelExporterOTLPEndpoint),
	)
}

// String e GoString fazem %v, %+v, %s e %#v passarem pela mesma redação;
// sem eles o fmt imprimiria a struct crua.
func (c Config) String() string   { return c.LogValue().String() }
func (c Config) GoString() string { return c.String() }

// redactDatabaseURL remove a senha da URL (userinfo e parâmetros de query
// com "password" no nome). Uma URL que não seja parseável como URL absoluta
// (malformada, ou no formato chave=valor) é redigida por inteiro: não há como
// saber onde está a senha, e falhar fechado é o comportamento seguro.
func redactDatabaseURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return Redacted
	}
	q := u.Query()
	for k := range q {
		if strings.Contains(strings.ToLower(k), "password") {
			q.Set(k, "xxxxx")
		}
	}
	u.RawQuery = q.Encode()
	return u.Redacted()
}
