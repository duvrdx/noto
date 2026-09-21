# architecture-boundary Specification

## Purpose
TBD - created by archiving change add-project-scaffolding. Update Purpose after archive.

## Requirements

### Requirement: `internal/core` não depende de infraestrutura

Nenhum pacote sob `internal/core/...` SHALL importar, direta ou transitivamente, bibliotecas de infraestrutura — nomeadamente `pgx`, qualquer cliente de Telegram e qualquer cliente de Ollama — nem qualquer pacote sob `internal/adapters/...`. As setas de `adapters` para `core` são sempre *implementa uma interface*, nunca *importa um tipo* (ADR 0001, PRD §8.2, `architecture.md` §3).

#### Scenario: Árvore limpa passa

- **Given** a árvore `internal/core/...` sem dependências de infraestrutura
- **When** o teste de fronteira arquitetural roda
- **Then** ele passa

#### Scenario: Import direto de infraestrutura falha

- **Given** um pacote em `internal/core/` que importa `github.com/jackc/pgx/v5`
- **When** o teste de fronteira arquitetural roda
- **Then** ele falha, nomeando o pacote violador e o import proibido

#### Scenario: Import transitivo também falha

- **Given** um pacote em `internal/core/` que importa outro pacote de `core` que por sua vez importa um cliente de Telegram
- **When** o teste de fronteira arquitetural roda
- **Then** ele falha e reporta a cadeia de imports completa até o import proibido

#### Scenario: Dependência de `core` sobre `adapters` falha

- **Given** um pacote em `internal/core/` que importa `internal/adapters/postgres`
- **When** o teste de fronteira arquitetural roda
- **Then** ele falha

### Requirement: A fronteira é verificada no CI

O teste de fronteira arquitetural SHALL rodar como parte de `go test ./...` no CI de todo PR, e uma violação SHALL reprovar o PR (`testing.md` §4 e §5). A verificação SHALL NOT depender de inspeção manual ou de revisão de código.

#### Scenario: Violação reprova o PR

- **Given** um PR que introduz um import proibido em `internal/core/`
- **When** o CI roda
- **Then** o job de teste falha e o PR não pode ser mesclado

### Requirement: Árvore de pacotes conforme o layout decidido

O repositório SHALL conter a árvore `cmd/noto/`, `internal/core/{item,revision,resolve,timex,ports}`, `internal/app/`, `internal/adapters/{telegram,ollama,postgres,otel}` e `internal/platform/{config,logging,http,migrations}`, conforme PRD §8.2 e `architecture.md` §10. Pacotes ainda sem implementação SHALL existir como pacotes Go compiláveis, para que a fronteira seja verificável desde já.

#### Scenario: Árvore compila

- **Given** o repositório recém-provisionado
- **When** `go build ./...` roda
- **Then** a compilação termina sem erro, incluindo os pacotes ainda vazios
