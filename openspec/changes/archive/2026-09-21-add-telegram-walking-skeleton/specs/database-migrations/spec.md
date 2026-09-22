# database-migrations

## ADDED Requirements

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
