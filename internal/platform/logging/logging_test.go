package logging_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/duvrdx/noto/internal/platform/logging"
)

func TestNewEmitsJSONWithLevelTimeAndMessage(t *testing.T) {
	var buf bytes.Buffer
	log, err := logging.New(&buf, "info")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Info("inicializando", "porta", 8080)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("saída não é JSON válido: %v\n%s", err, buf.String())
	}
	if rec["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", rec["level"])
	}
	if rec["msg"] != "inicializando" {
		t.Errorf("msg = %v, want inicializando", rec["msg"])
	}
	if _, ok := rec["time"]; !ok {
		t.Error("registro sem time")
	}
	if rec["porta"] != float64(8080) {
		t.Errorf("porta = %v, want 8080", rec["porta"])
	}
	if strings.Count(buf.String(), "\n") != 1 {
		t.Errorf("esperava exatamente uma linha, veio: %q", buf.String())
	}
}

func TestNewFiltersByLevel(t *testing.T) {
	tests := []struct {
		level     string
		wantDebug bool
		wantInfo  bool
		wantWarn  bool
		wantError bool
	}{
		{"debug", true, true, true, true},
		{"info", false, true, true, true},
		{"warn", false, false, true, true},
		{"error", false, false, false, true},
		{"INFO", false, true, true, true}, // case-insensitive
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			var buf bytes.Buffer
			log, err := logging.New(&buf, tt.level)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			log.Debug("d")
			log.Info("i")
			log.Warn("w")
			log.Error("e")

			out := buf.String()
			for msg, want := range map[string]bool{"d": tt.wantDebug, "i": tt.wantInfo, "w": tt.wantWarn, "e": tt.wantError} {
				if got := strings.Contains(out, `"msg":"`+msg+`"`); got != want {
					t.Errorf("msg %q emitida = %v, want %v (saída: %s)", msg, got, want, out)
				}
			}
		})
	}
}

func TestNewInfoSuppressesDebugCompletely(t *testing.T) {
	var buf bytes.Buffer
	log, err := logging.New(&buf, "info")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Debug("ruído")
	if buf.Len() != 0 {
		t.Errorf("debug com LOG_LEVEL=info deveria não emitir nada, veio: %s", buf.String())
	}
}

func TestNewRejectsInvalidLevel(t *testing.T) {
	for _, level := range []string{"verbose", "", "info+2", "trace"} {
		t.Run(level, func(t *testing.T) {
			var buf bytes.Buffer
			log, err := logging.New(&buf, level)
			if err == nil {
				t.Fatalf("New(%q) sem erro", level)
			}
			if log != nil {
				t.Errorf("logger deveria ser nil em caso de erro")
			}
			if !strings.Contains(err.Error(), "LOG_LEVEL") {
				t.Errorf("erro %q deveria citar LOG_LEVEL", err.Error())
			}
		})
	}
}
