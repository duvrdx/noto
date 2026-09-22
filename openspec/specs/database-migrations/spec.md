# database-migrations Specification

## Purpose
TBD - created by archiving change add-project-scaffolding. Update Purpose after archive.

## Requirements

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

### Requirement: Migração de domínio do M1 cria `users` e `messages`

A migração `migrations/00002_create_users_and_messages.sql` SHALL criar exatamente as tabelas `users` e `messages`, e SHALL conter **somente as colunas que o M1 lê ou escreve** (PRD §6.2 é o alvo final; cada coluna omitida é introduzida pelo marco que a usa). O esquema SHALL ser:

- `users`: `id uuid` chave primária com default `gen_random_uuid()`; `telegram_user_id bigint NOT NULL UNIQUE`; `created_at timestamptz NOT NULL` com default `now()`.
- `messages`: `id uuid` chave primária com default `gen_random_uuid()`; `user_id uuid NOT NULL` com chave estrangeira para `users(id)`; `telegram_update_id bigint NOT NULL UNIQUE`; `chat_id bigint NOT NULL`; `raw_text text NOT NULL`; `received_at timestamptz NOT NULL` com default `now()`.

O arquivo SHALL ter blocos `-- +goose Up` e `-- +goose Down`; o `Down` SHALL remover `messages` antes de `users` e SHALL NOT remover a extensão `pg_trgm`.

#### Scenario: Colunas e restrições conferem

- **Given** um Postgres vazio
- **When** `noto migrate` roda
- **Then** `users` e `messages` existem com exatamente as colunas, tipos, nulidade e restrições acima, e nenhuma coluna além delas

#### Scenario: `telegram_update_id` duplicado é rejeitado pelo próprio banco

- **Given** uma linha em `messages` com `telegram_update_id = 42`
- **When** uma segunda linha com `telegram_update_id = 42` é inserida
- **Then** o banco rejeita por violação de unicidade, sem depender de nenhuma checagem da aplicação

#### Scenario: Mensagem exige usuário existente

- **Given** nenhuma linha em `users` com determinado `id`
- **When** se tenta inserir uma `messages` com esse `user_id`
- **Then** o banco rejeita por violação de chave estrangeira

#### Scenario: Colunas e tabelas de outros marcos não existem

- **Given** o banco migrado
- **When** o catálogo é consultado
- **Then** `users` não tem `timezone` nem `locale`, `messages` não tem `telegram_message_id`, e nenhuma das tabelas `items`, `reminders`, `outbox`, `pending_questions`, `item_revisions`, `parse_runs` e `resolutions` existe

#### Scenario: Reversão remove as duas tabelas e preserva a extensão

- **Given** o banco migrado até a `00002`
- **When** o `Down` da `00002` é aplicado
- **Then** `messages` e `users` deixam de existir e `pg_trgm` continua em `pg_extension`

#### Scenario: A migração inicial continua sem tabelas

- **Given** um Postgres vazio
- **When** apenas a migração `00001_enable_pg_trgm.sql` é aplicada
- **Then** nenhuma tabela de domínio existe (o requirement "Primeira migração habilita `pg_trgm`" segue verdadeiro para a `00001`)

### Requirement: Queries do M1 escritas à mão e geradas com sqlc

As queries do M1 SHALL viver em `internal/adapters/postgres/queries/` como SQL escrito à mão (ADR 0001, sem ORM), e o código gerado SHALL ser versionado em `internal/adapters/postgres/db/`. Deve existir uma query que faz upsert de `users` por `telegram_user_id` devolvendo o `id`, e uma que insere em `messages` com `ON CONFLICT (telegram_update_id) DO NOTHING` devolvendo o `id` apenas quando a linha é nova. A query `Ping` do scaffolding SHALL permanecer.

#### Scenario: Geração é reprodutível

- **Given** o repositório com as queries do M1
- **When** `sqlc generate` roda e em seguida `go build ./...`
- **Then** ambos encerram com sucesso e `git diff` não mostra nenhuma alteração no código gerado

#### Scenario: Insert de mensagem repetida não devolve linha

- **Given** uma mensagem já gravada com determinado `telegram_update_id`
- **When** a query de insert roda de novo com o mesmo `telegram_update_id`
- **Then** nenhuma linha é devolvida e nenhuma linha nova existe em `messages`
