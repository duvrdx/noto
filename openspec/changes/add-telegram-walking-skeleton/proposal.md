# M1 — Walking skeleton: Telegram → Postgres → eco

## Why

Ver *Intent* logo abaixo: o M1 é o primeiro marco com produto de verdade (uma mensagem no Telegram é persistida no Postgres e recebe resposta), e é onde idempotência de entrada e privacidade nos logs precisam ser provadas antes de o sistema passar a agir sem confirmação.

## What Changes

Ver *Scope* e *Impacto nas specs* abaixo: polling + `IngestMessage` + migração de `users`/`messages` + token obrigatório + infraestrutura de teste com Postgres real, tudo sem parser, itens, lembretes, outbox nem webhook.

## Intent

O scaffolding entregou um binário que sobe, lê configuração e não faz nada de produto. O M1 (PRD §14) é o primeiro marco com produto de verdade: **uma mensagem enviada ao bot no Telegram é persistida no Postgres e recebe uma resposta**. Critério de pronto do PRD: *"Mensagem aparece em `messages` e recebe resposta"*.

Um *walking skeleton* existe para atravessar **todas as camadas com a menor fatia possível**, de modo que as decisões estruturais (fronteira `core`/`app`/`adapters`, portas, transação, idempotência, privacidade nos logs) sejam exercitadas uma vez, com teste, antes de qualquer lógica de produto se apoiar nelas. Sem LLM, sem parser, sem itens, sem lembretes.

O ponto que justifica o cuidado desta proposta: o sistema, a partir do M3, **age sem confirmação prévia** (ADR 0006). Por isso a idempotência de entrada e a política de logs são o conteúdo real do M1 — o eco é só o pretexto para provar que elas funcionam.

## Scope

### Dentro

1. **Transporte por long polling** (`TELEGRAM_TRANSPORT=polling`, ADR 0008) rodando dentro do processo `noto serve`, com `github.com/go-telegram/bot`. Somente mensagens de texto de chat privado são processadas; todo o resto é descartado com log de identificadores.
2. **Caso de uso `IngestMessage`** (`internal/app`): persiste `users` + `messages` numa transação, e responde com o **eco** do texto recebido pela porta `ports.Notifier`.
3. **Idempotência de entrada** por `messages.telegram_update_id UNIQUE` (PRD §8.4, ADR 0008 §2): update reentregue não duplica registro **nem responde de novo**.
4. **Primeira migração de domínio** `00002_create_users_and_messages.sql`, **só com as colunas que o M1 usa** (ver `design.md` §2.5). Queries reais em `internal/adapters/postgres/queries/` e código `sqlc` versionado.
5. **`TELEGRAM_BOT_TOKEN` passa a ser obrigatório** (modificação da spec `configuration`; Questão em aberto nº 4 do scaffolding).
6. **Privacidade estrutural**: o texto da mensagem nunca aparece em log; o token nunca aparece em log, em erro nem em `stderr` — inclusive quando a biblioteca de Telegram tenta logar o payload de um update malformado ou uma URL de API que carrega o token no caminho.
7. **Infraestrutura de teste do M1**: helper de Postgres real com `testcontainers-go` (primeira vez que o repositório exige Docker nos testes), fake da API do Telegram por `httptest`, coletor em memória para `ports.Notifier`.
8. **Guarda arquitetural estendida a `internal/app/...`** (hoje o teste de fronteira só cobre `core`).
9. **Verificação ponta a ponta com o bot real**, executada pelo usuário (só ele tem o token).

### Fora (omissões deliberadas, não esquecimentos)

- **Webhook.** `TELEGRAM_TRANSPORT=webhook` continua sendo um valor aceito pela configuração, mas o `serve` **recusa iniciar** com erro claro. Recomendação e alternativa em `design.md` §2.1 e na Questão em aberto nº 1.
- **Outbox** e despachante. A resposta é enviada direto, depois do commit. Exceção temporária ao ADR 0003, argumentada em `design.md` §2.2; some no M4. Questão em aberto nº 2.
- Parser/Ollama, `items`, `reminders`, `pending_questions`, `item_revisions`, `resolutions`, `parse_runs`, `outbox` (tabela), resolver, scheduler real, `/hoje`, callbacks/botões. Nenhuma dessas tabelas nasce.
- **Onboarding e captura de fuso.** `/start` é tratado como texto comum e ecoado. `users.timezone` **não** entra no M1 (`design.md` §2.5.1).
- **OpenTelemetry.** Sem instrumentação no M1 (o `lgtm` continua ocioso). Reverte uma frase do `design.md` do scaffolding §10; ver Questão em aberto nº 7.
- `/esquecer`, retenção/expurgo, rate limit por usuário, readiness check (`Ping`).
- Qualquer mudança em `docker-compose.yml`, `Dockerfile` ou `.github/workflows/ci.yml` (verificado em `design.md` §2.10: nenhuma é necessária).

## Approach

Três camadas, cada uma com um arquivo de teste que a prova isoladamente, e um teste que as prova juntas:

```
adapters/telegram  ──update──▶  app.IngestMessage  ──ports.MessageStore──▶  adapters/postgres
   (polling)                         │
                                     └──ports.Notifier──▶  adapters/telegram (sendMessage)
```

- `internal/core/message` define `Incoming` (o que o domínio sabe de uma mensagem que chegou) e suas invariantes. `internal/core/ports` declara `MessageStore` e `Notifier`. `core` continua sem importar `pgx`, Telegram ou `adapters` (o teste de fronteira do scaffolding é a prova).
- `internal/app.IngestMessage`: valida → `MessageStore.SaveIncoming` (usuário + mensagem, idempotente, numa transação do adapter) → se a mensagem é nova, `Notifier.Notify(chatID, texto)`.
- `internal/adapters/postgres` implementa `MessageStore` com `sqlc` e uma transação `pgx`. O `INSERT ... ON CONFLICT (telegram_update_id) DO NOTHING` é a idempotência: quem dispara o eco é o banco dizendo "esta linha é nova", não uma checagem em memória.
- `internal/adapters/telegram` encapsula `go-telegram/bot`: mapeia `models.Update` → `message.Incoming`, filtra o que não é texto privado, entrega **serialmente e em ordem** ao caso de uso, e implementa `Notifier` com `sendMessage`. Ele **substitui todos os handlers padrão da biblioteca** (que logam o update inteiro e usam o `log` global) por versões que só emitem identificadores.
- `cmd/noto serve` compõe tudo: pool `pgx` → cliente Telegram → caso de uso → poller, ao lado do `GET /healthz` que já existe. `worker` não muda.
- Prova ponta a ponta automática: um teste em `cmd/noto` sobe Postgres real (testcontainers) e uma API de Telegram falsa, injeta um update, e confere linha em `messages`, uma resposta enviada, e — reinjetando o mesmo update — nenhuma linha nova e nenhuma segunda resposta. A verificação com o bot real (task 10.1) valida o que o fake não pode: o token, a rede e o Telegram de verdade.

Nova dependência de produção: `github.com/go-telegram/bot v1.27.0` (`go 1.18`, zero dependências transitivas). Novas dependências de teste: `testcontainers-go` e `testcontainers-go/modules/postgres`, ambas `v0.44.0` (`go 1.25.0`). Nenhuma sobe a linha `go` de 1.26.0; `govulncheck -test` num módulo-amostra com essas versões saiu limpo. Detalhes em `design.md` §2.11.

## Impacto nas specs

| Capability | Operação | Observação |
|---|---|---|
| `configuration` | **REMOVED + ADDED** | Token obrigatório: remove o requirement do scaffolding "apenas `DATABASE_URL` é obrigatória" e adiciona "Variáveis obrigatórias" (ver abaixo) |
| `database-migrations` | ADDED | `users` e `messages`; queries `sqlc` |
| `message-ingestion` | ADDED (nova) | Caso de uso, idempotência, falhas, logs |
| `telegram-transport` | ADDED (nova) | Polling, filtros, ACK, privacidade, recusa do webhook |
| `local-environment` | ADDED | Postgres real nos testes; CI com Docker; `.env.example` |
| `architecture-boundary` | ADDED | `internal/app` também sob guarda |

### Armadilha das specs base — resolvida

`add-project-scaffolding` foi **arquivado em 2026-09-21** (task 8.2 aprovada pelo usuário), e `openspec/specs/` passou a ter as cinco capabilities dele. Com a base disponível, o `openspec validate --strict` recusou o delta `MODIFIED`: ele exigia copiar de volta dois cenários do requirement antigo ("`.env` recém-copiado sobe o serve" e "Token de Telegram vazio não é erro") que **contradizem** a nova regra do token. Por isso `configuration` usa **`REMOVED` (com Reason e Migration) + `ADDED`**, e não `RENAMED` + `MODIFIED`. As demais capabilities deste change são `ADDED` e não tinham o problema.
