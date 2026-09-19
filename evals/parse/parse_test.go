//go:build eval

// Package parse conterá o eval de extração (frase -> JSON estruturado), que
// chama o LLM e portanto só roda sob a tag eval (eval-strategy.md).
package parse

import "testing"

// TestPlaceholder não assere nada: só garante que o pacote compila e que
// `make eval-parse` encerra com código 0 até os cenários chegarem (M2).
func TestPlaceholder(t *testing.T) {}
