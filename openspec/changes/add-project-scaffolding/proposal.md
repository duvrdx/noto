# Scaffolding inicial do Noto

## Intent

O M0 está aprovado: PRD, arquitetura, estratégia de teste, estratégia de eval e oito ADRs. Não existe uma linha de código. O M1 (*walking skeleton*: webhook recebe → persiste → ecoa) precisa de um chão sobre o qual ser construído, e esse chão já está inteiramente decidido nos documentos do M0 — §8.2 do PRD e §10 de `architecture.md` descrevem o layout, o ADR 0001 escolhe as bibliotecas, `testing.md` §5 define o CI, `eval-strategy.md` §7 define os alvos de eval.

Esta mudança materializa essas decisões como estrutura executável. Nada aqui é novo: é a tradução do M0 em arquivos. Qualquer coisa que exija uma decisão inédita está listada em *Decisões tomadas* ou em *Questões em aberto* no `tasks.md`, não resolvida em silêncio dentro de uma task.

O critério de pronto é modesto e verificável por máquina: **compila, sobe com `docker compose up`, e `go test ./...` passa com os testes que existem** — incluindo o teste de fronteira arquitetural, que é o único teste com conteúdo real nesta entrega.

## Scope

### Dentro

1. `go.mod` com o módulo `github.com/duvrdx/noto` (derivado do remoto `origin`) e a diretiva `go 1.22` — o mínimo declarado pelo ADR 0001, decidido pelo usuário.
2. `cmd/noto/` — binário único com os subcomandos `serve`, `worker` e `migrate` (ADR 0001, PRD §8.1). Os três iniciam, leem configuração, logam e encerram por sinal. Nenhum deles faz trabalho de domínio ainda.
3. Árvore de pacotes `internal/core`, `internal/app`, `internal/adapters`, `internal/platform` conforme PRD §8.2 e `architecture.md` §10.
4. `docker-compose.yml` — `postgres:16` com `pg_trgm` habilitado e `grafana/otel-lgtm` (PRD §8.3 e §10), mais os serviços `serve` e `worker` como processos distintos coordenando apenas pelo banco (ADR 0001). Postgres publicado no host em `5432`, `serve` em `8080`; dentro da rede do Compose, `DATABASE_URL` e `OTEL_EXPORTER_OTLP_ENDPOINT` são sobrescritos para os hosts `postgres` e `lgtm`, sem alterar o `.env.example`.
5. `Makefile` com os alvos `build`, `test`, `lint`, `migrate` e `eval` (com `eval-parse` e `eval-resolve` separados, por `eval-strategy.md` §7).
6. `sqlc.yaml` e `migrations/` com `goose` — a primeira migração cria apenas a extensão `pg_trgm`. **O schema do PRD §6.2 não entra aqui**; ele pertence ao M1 em diante, junto do código que o usa.
7. `internal/platform/config` lendo exatamente as variáveis já fixadas em `.env.example`, e `internal/platform/logging` com `log/slog` em JSON.
8. **Teste de fronteira arquitetural** que falha se `internal/core/...` importar `pgx`, `telegram` ou `ollama` — exigência nominal do ADR 0001 (*Consequências → custo*), de `architecture.md` §3 e de `testing.md` §4.
9. CI rodando `go vet`, `staticcheck`, `go test -race ./...` e `make eval-resolve` — os quatro passos que `testing.md` §5 exige em todo PR. `eval-parse` fica fora.
10. Normalização do `.env.example`: comentários inline movidos para linhas próprias, porque comentário depois do valor é lido como parte do valor por `env_file` do Compose e por `set -a; . arquivo`. Os valores não mudam.

### Fora

Tudo que é M1 ou posterior: lógica de domínio, entidades, o parser, o resolver, o scheduler, a outbox, o adapter do Telegram, as tabelas do PRD §6.2, os conjuntos de eval. As pastas dessas coisas são criadas; o conteúdo, não.

Especificamente fora, embora tentador:
- **Schema do domínio em `migrations/`.** Migração escrita antes do código que a consome envelhece errado.
- **Cliente Ollama, ainda que vazio.** Não há `ports.Parser` para ele implementar até o M2.
- **`evals/cases` e `evals/scenarios` povoados.** O alvo `eval` do Makefile existe e roda contra um conjunto vazio; o conjunto é M2.

## Approach

Binário único com subcomandos via `flag` da stdlib e um `switch` em `os.Args[1]` — coerente com a recusa a frameworks do ADR 0001; não há caso de uso para Cobra em três subcomandos.

`main` faz três coisas: carrega config, monta o logger, e despacha para o subcomando com um `context.Context` cancelado por `SIGINT`/`SIGTERM`. Cada subcomando é uma função `run*(ctx, cfg, logger) error` — `serve` sobe um `net/http.Server` com um `/healthz`, `worker` roda um tick vazio, `migrate` aplica `goose` contra `DATABASE_URL`.

A configuração é uma struct única preenchida a partir do ambiente e **validada na inicialização**: variável obrigatória ausente derruba o processo com mensagem nomeando a variável, em vez de falhar mais tarde com um erro obscuro. No scaffolding a única obrigatória é `DATABASE_URL` (mais `TELEGRAM_WEBHOOK_SECRET` quando `TELEGRAM_TRANSPORT=webhook`); `TELEGRAM_BOT_TOKEN` vazio não impede o `serve` de subir, porque ainda não há adapter de Telegram que o use — ele endurece no M1. Segredos nunca são logados (PRD §12).

O teste de fronteira usa `golang.org/x/tools/go/packages` para carregar `./internal/core/...` e inspecionar os imports transitivos de cada pacote, falhando com o caminho completo da cadeia que violou. É o único teste desta entrega com asserção de verdade, e ele é o que impede a erosão que o próprio ADR 0001 prevê.

Compose e migrações são o par que torna o resultado verificável: `docker compose up` sobe Postgres e LGTM, `make migrate` aplica a extensão, `make build && make test` fecha o ciclo.

## Dependência de ambiente — leia antes de aprovar

Estado verificado do ambiente de desenvolvimento:

| Ferramenta | Estado | Impacto |
|---|---|---|
| `go` | **instalado, 1.27.1**, em `/usr/local/go/bin` — mas **fora do `PATH`** | `go build`, `go vet`, `go test` rodam desde que o `PATH` seja exportado antes |
| `docker` / `docker compose` | **funcionando** — Docker 28.5.1, Compose v2.40 | `docker compose up` é verificável |
| `sqlc`, `goose`, `staticcheck` | **ausentes** | `make migrate`, `make lint` e `sqlc generate` não rodam até a task 1.3 |

As tasks 1.1 (Go ≥ 1.22) e 1.2 (Docker) estão **satisfeitas** e marcadas como concluídas no `tasks.md`, com a evidência registrada. Resta a task **1.3** como único pré-requisito de ferramentas — e ela é `owner: agent`, por ser um `go install` mecânico que depende apenas da 1.1, já satisfeita. As tasks 1.1 e 1.2 eram `owner: human` porque instalavam software na máquina do usuário; a 1.3 não compartilha essa característica no estado atual.

**Ressalva que atravessa todo o plano:** `/usr/local/go/bin` não está no `PATH` do shell do usuário. Todo comando Go precisa ser precedido de

```sh
export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH
```

O `Makefile` desta entrega faz esse export internamente, de modo que `make <alvo>` funcione num shell limpo. O `~/.zshrc` do usuário **não** é alterado por nenhuma task.

Com isso, o critério de pronto desta mudança — compila, sobe com `docker compose up`, `go test ./...` passa — é **verificável no ambiente atual** assim que a 1.3 estiver feita.

## Primeiro commit — decidido

O repositório não tem nenhum commit. `.env.example`, `.gitignore`, `README.md` e `docs/` inteiro estão untracked, e o `openspec init` desta mudança adicionou `openspec/` e `.claude/` (6 skills, 6 commands).

O usuário decidiu a **opção (b)**: M0 e adoção do OpenSpec em **commits separados**, nessa ordem, antes de qualquer commit de código —

1. `docs(m0):` — `README.md`, `docs/` inteiro, `.gitignore`, `.env.example`;
2. `chore(openspec):` — `openspec/` (com `--language pt-BR` mantido em `openspec/config.yaml`) e `.claude/`, que **é commitado** por decisão do usuário.

A task 0.1 executa isso e permanece `owner: human`: é a inauguração do histórico do repositório, um ato de curadoria que nenhum teste verifica.

## Outras decisões do usuário incorporadas

- **Diretiva `go` do `go.mod`: `go 1.22`** — o piso do ADR 0001, não a versão do toolchain instalado.
- **Convenção de commit:** Conventional Commits com escopo, e toda mensagem terminando com `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`.
- **Portas do Compose:** Postgres publicado no host em `5432`, `serve` em `8080`.
- **Variáveis obrigatórias:** no scaffolding, apenas `DATABASE_URL`. Um `.env` recém-copiado de `.env.example`, com um `DATABASE_URL` válido, sobe o `serve` — `TELEGRAM_BOT_TOKEN` vazio só passa a ser impeditivo no M1.
- **CI:** inclui `make eval-resolve` em todo PR, como manda `testing.md` §5, ainda que hoje o alvo seja um no-op verde.

## Referências

- PRD §8.1, §8.2, §8.3, §8.8, §9, §10, §12, §14 (M1)
- `architecture.md` §2, §3, §10
- `testing.md` §4 (Fronteira arquitetural), §5 (CI)
- `eval-strategy.md` §7 (alvos de eval)
- ADR 0001 (Go, monólito modular, binário único), ADR 0003 (Postgres como fila), ADR 0008 (transporte e deploy)
