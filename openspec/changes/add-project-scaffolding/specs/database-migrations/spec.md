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

O repositório SHALL conter um `sqlc.yaml` apontando para `migrations/` como fonte de schema e para o diretório de queries do adapter Postgres, gerando código com o driver `pgx` (ADR 0001 — SQL à mão, sem ORM). A configuração SHALL ser válida mesmo sem nenhuma query ainda presente.

#### Scenario: Geração roda com zero queries

- **Given** o repositório provisionado, sem nenhum arquivo de query
- **When** `sqlc generate` roda
- **Then** o comando encerra com sucesso e não produz erro de configuração
