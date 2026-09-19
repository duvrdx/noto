package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// New devolve um logger slog com handler JSON gravando em w, filtrado por
// level (LOG_LEVEL): debug, info, warn ou error, sem distinguir caixa.
// Qualquer outro valor é erro explícito — nunca um fallback silencioso.
//
// O logger é construído e devolvido, jamais instalado como global: quem o
// recebe o injeta nos subcomandos.
func New(w io.Writer, level string) (*slog.Logger, error) {
	lvl, err := parseLevel(level)
	if err != nil {
		return nil, err
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})), nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("LOG_LEVEL: valor %q inválido, aceitos: debug, info, warn, error", s)
}
