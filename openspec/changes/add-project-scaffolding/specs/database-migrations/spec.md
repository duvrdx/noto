# database-migrations

## ADDED Requirements

### Requirement: Migrações versionadas com goose

O schema do banco SHALL ser gerenciado por `goose`, com as migrações versionadas em `migrations/` (ADR 0001, PRD §8.3, `architecture.md` §10). Aplicar migrações SHALL ser possível tanto por `noto migrate` quanto por `make migrate`, ambos usando `DATABASE_URL`.

#### Scenario: Migração aplicada em banco limpo

- **Given** um Postgres vazio acessível por `DATABASE_URL`
- **When** `noto migrate` roda
- **Then** as migrações pendentes são aplicadas e a tabela de versões do goose registra a versão atual

#### Scenario: Reexecução é inerte

- **Given** um banco já migrado até a última versão
- **When** `noto migrate` roda novamente
- **Then** nenhuma migração é reaplicada e o comando encerra com sucesso

### Requirement: Primeira migração habilita `pg_trgm`

A migração inicial SHALL criar a extensão `pg_trgm`, de que depende a resolução de referência (PRD §8.3, ADR 0007), e SHALL NOT criar nenhuma tabela do modelo de domínio do PRD §6.2 — essas migrações pertencem aos marcos que introduzem o código que as usa.

#### Scenario: Extensão criada

- **Given** um Postgres vazio
- **When** a migração inicial é aplicada
- **Then** `pg_trgm` está presente em `pg_extension`

#### Scenario: Nenhuma tabela de domínio é criada

- **Given** um Postgres vazio
- **When** a migração inicial é aplicada
- **Then** nenhuma das tabelas de PRD §6.2 (`users`, `items`, `reminders`, `outbox` e demais) existe

### Requirement: Geração de código com sqlc configurada

O repositório SHALL conter um `sqlc.yaml` apontando para `migrations/` como fonte de schema e para o diretório de queries do adapter Postgres, gerando código com o driver `pgx` (ADR 0001 — SQL à mão, sem ORM). O `sqlc` (v1.31.1) recusa uma configuração sem nenhuma query (`no queries contained in paths`), portanto o scaffolding SHALL incluir ao menos uma query real, `Ping` (`SELECT 1`), que servirá depois a um readiness check. `sqlc generate` SHALL terminar com sucesso e o código gerado, versionado em `internal/adapters/postgres/db/`, SHALL compilar contra a versão de `pgx` do `go.mod`. O `sqlc` SHALL ignorar o bloco `-- +goose Down` ao ler o schema.

#### Scenario: Geração roda e o código compila

- **Given** o repositório provisionado, com a query `Ping` em `internal/adapters/postgres/queries/`
- **When** `sqlc generate` roda e em seguida `go build ./...`
- **Then** ambos encerram com sucesso e o código gerado compila

#### Scenario: Diretório de queries sem nenhuma query é recusado pelo sqlc

- **Given** o diretório de queries sem nenhum arquivo de query
- **When** `sqlc generate` roda
- **Then** o `sqlc` falha com `no queries contained in paths`, e é por isso que a query `Ping` existe
