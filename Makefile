# O toolchain Go pode não estar no PATH do shell (instalado em /usr/local/go/bin).
# O repositório se basta: o export vive aqui, não no perfil do usuário.
export PATH := /usr/local/go/bin:$(HOME)/go/bin:$(PATH)

# Repassados como variáveis de ambiente aos alvos de eval (eval-strategy.md §7).
# O consumo é do M2; aqui só o encanamento existe.
export CATEGORY
export MODEL

.PHONY: build test lint migrate eval eval-parse eval-resolve

build:
	go build -o bin/noto ./cmd/noto

test:
	go test -race ./...

lint:
	go vet ./...
	staticcheck ./...

# DATABASE_URL vem do ambiente; se não estiver definida, cai para o .env.
# (nenhuma variável nova: é a mesma DATABASE_URL do .env.example)
migrate:
	@if [ -z "$$DATABASE_URL" ] && [ -f .env ]; then set -a; . ./.env; set +a; fi; \
	go run ./cmd/noto migrate

eval: eval-parse eval-resolve

# chama serviço externo pago: só quando muda prompt ou modelo (fora do CI padrão)
eval-parse:
	go test -count=1 -tags eval ./evals/parse/...

# determinístico, não chama LLM; roda em todo PR
eval-resolve:
	go test -count=1 -tags eval ./evals/resolve/...
