//go:build eval

// Package resolve conterá o eval de resolução de referência: determinístico,
// sem chamar LLM, roda em todo PR (eval-strategy.md, ADR 0007).
package resolve

import "testing"

// TestPlaceholder não assere nada: só garante que o pacote compila e que
// `make eval-resolve` encerra com código 0 com zero cenários, porque o CI já
// o executa em todo PR (task 8.1). Os cenários chegam no M2.
func TestPlaceholder(t *testing.T) {}
