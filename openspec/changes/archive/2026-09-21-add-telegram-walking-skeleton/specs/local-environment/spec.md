# local-environment

## ADDED Requirements

### Requirement: Testes de integração usam Postgres real via testcontainers

Testes que exercitam repositórios e casos de uso SHALL usar um Postgres real (`postgres:16`, com `pg_trgm` disponível) subido por `testcontainers-go`, conforme `testing.md` §2–§3; repositórios SHALL NOT ser substituídos por dublês. O helper `internal/testutil/testdb` SHALL subir **um** contêiner por execução de pacote de teste, aplicar as migrações do repositório **uma vez**, e entregar a cada teste uma transação com *rollback* ao final. Um teste que precise de visibilidade entre conexões (concorrência, commit real) SHALL poder pedir um pool sobre um banco recém-criado no mesmo contêiner. O contêiner SHALL rodar em UTC, deliberadamente (`testing.md` §4).

Sem Docker disponível, um teste de banco SHALL **falhar** com mensagem que diz que o Docker é necessário — nunca passar em silêncio. Com `go test -short`, os testes que precisam de Docker SHALL ser **pulados** explicitamente (`t.Skip`), para manter o ciclo de desenvolvimento rápido.

#### Scenario: Isolamento por rollback

- **Given** dois testes consecutivos usando o helper
- **When** o primeiro insere uma linha em `users` e termina
- **Then** o segundo não enxerga essa linha

#### Scenario: Migrações aplicadas e extensão disponível

- **Given** o helper em uso
- **When** um teste consulta o catálogo
- **Then** `users`, `messages` e a extensão `pg_trgm` existem

#### Scenario: Sem Docker o teste falha alto

- **Given** um ambiente sem daemon Docker acessível, e `go test` sem `-short`
- **When** um teste de banco roda
- **Then** o teste falha com uma mensagem que cita o Docker

#### Scenario: `-short` pula o que exige Docker

- **Given** `go test -short ./...`
- **When** a suíte roda
- **Then** os testes que exigem Docker aparecem como pulados e os demais executam

### Requirement: O CI executa os testes de banco sem passos adicionais

O workflow de CI existente SHALL executar os testes que usam testcontainers no passo `go test -race ./...` do runner `ubuntu-latest`, que dispõe de Docker, sem serviço `postgres` declarado no workflow e sem alteração do arquivo `ci.yml`. O passo `govulncheck ./...` SHALL continuar bloqueante e SHALL passar com as dependências novas.

#### Scenario: PR verde inclui os testes com Postgres real

- **Given** um PR do M1 sem violações
- **When** o CI roda
- **Then** os testes com testcontainers executam e passam, e `govulncheck ./...` encerra com código zero

### Requirement: `.env.example` documenta que o token é obrigatório

`.env.example` SHALL conter, em linha própria acima de `TELEGRAM_BOT_TOKEN`, um comentário dizendo que a variável é obrigatória e onde obtê-la (`@BotFather`), **sem valor de exemplo**. Nenhum valor do arquivo SHALL mudar, e o arquivo SHALL continuar carregável sem transformação (requirement existente).

#### Scenario: Comentário presente e valor intacto

- **Given** `.env.example`
- **When** ele é lido e carregado com `set -a; . ./.env.example; set +a`
- **Then** o comentário obrigatório está na linha imediatamente acima de `TELEGRAM_BOT_TOKEN`, a variável carrega vazia, e `TELEGRAM_TRANSPORT` continua valendo exatamente `polling`
