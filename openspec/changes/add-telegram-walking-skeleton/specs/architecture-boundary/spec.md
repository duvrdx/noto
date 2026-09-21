# architecture-boundary

## ADDED Requirements

### Requirement: `internal/app` não depende de infraestrutura

Nenhum pacote sob `internal/app/...` SHALL importar, direta ou transitivamente, `pgx`, qualquer cliente de Telegram, qualquer cliente de Ollama, nem qualquer pacote sob `internal/adapters/...`. Casos de uso SHALL falar com o mundo exterior apenas por portas de `internal/core/ports` (ADR 0001, PRD §8.2). A verificação SHALL ser feita pelo mesmo teste de fronteira que já guarda `internal/core/...`, estendido a este subárvore, e SHALL considerar apenas código de produção (arquivos `_test.go` de `app` podem importar adapters para montar o cenário).

#### Scenario: Árvore de `app` limpa passa

- **Given** `internal/app/...` importando apenas `core` e a stdlib
- **When** o teste de fronteira roda
- **Then** ele passa

#### Scenario: Import de adapter em `app` falha

- **Given** um pacote em `internal/app/` que importa `internal/adapters/postgres`
- **When** o teste de fronteira roda
- **Then** ele falha, nomeando o pacote violador e a cadeia até o import proibido

#### Scenario: Import de cliente de Telegram em `app` falha

- **Given** um pacote em `internal/app/` que importa `github.com/go-telegram/bot`
- **When** o teste de fronteira roda
- **Then** ele falha, nomeando o pacote e o import proibido

#### Scenario: Testes de `app` podem importar adapters

- **Given** um `_test.go` em `internal/app/` que importa `internal/adapters/postgres` para montar um repositório real
- **When** o teste de fronteira roda
- **Then** ele passa, porque arquivos de teste não são analisados
