# Design — scaffolding inicial

## 1. O que este documento decide (e o que não)

Só decide o que o M0 deixou em aberto **por ser detalhe de implementação**. Tudo que o PRD, a arquitetura ou um ADR já fixaram é citado, não redecidido. O que exigiu decisão nova e não trivial foi levado ao usuário e está registrado em *Decisões tomadas* no `tasks.md`; o que continua em aberto está em *Questões em aberto*, lá também.

## 1.1 Nota de ambiente para quem executa

O toolchain Go instalado é o **1.27.1**, em `/usr/local/go/bin`, **fora do `PATH` do shell do usuário**. Todo comando `go`, `sqlc`, `goose` ou `staticcheck` precisa ser precedido de:

```sh
export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH
```

**Decisão: o export vive no `Makefile`, não no perfil do shell.** O `Makefile` define `PATH` internamente, de modo que `make build`/`test`/`lint`/`migrate` funcionem num shell limpo. Alterar o `~/.zshrc` do usuário seria mudar a máquina dele para acomodar um repositório — o repositório é que deve se bastar. *Consequência aceita:* comandos `go` avulsos, fora do `make`, continuam exigindo o export manual; por isso ele está também no `tasks.md` e no `README.md`.

A diretiva do `go.mod` é **`go 1.22`** — o piso do ADR 0001 — e não a versão do toolchain. Toolchain novo compilando diretiva antiga é o caso normal do Go; subir a diretiva só fecharia a porta a quem tem Go mais antigo, sem ganho nesta entrega. *Atualização (2026-09-19): o piso do módulo subiu de `go 1.22` para `go 1.26.0` por decisão do usuário, para corrigir GO-2026-5004 (pgx, SQL injection) e GO-2026-5970 (x/text, laço infinito) — ver o commit `chore(deps): eleva piso do Go para 1.26 e corrige vulnerabilidades`.*

## 2. Entrypoint e subcomandos

`cmd/noto/main.go` despacha em `os.Args[1]` para `runServe`, `runWorker` ou `runMigrate`, cada um com assinatura `func(ctx context.Context, cfg config.Config, log *slog.Logger) error`. Flags por subcomando, quando houver, usam `flag.NewFlagSet`.

**Decisão: stdlib `flag`, não Cobra/urfave.** Três subcomandos sem hierarquia, sem completion, sem flags compartilhadas complexas. O ADR 0001 rejeita framework web pelo mesmo raciocínio; um framework de CLI aqui seria a mesma dependência sem o mesmo problema. *Alternativa rejeitada:* Cobra — se o número de subcomandos crescer muito além disso, a troca é local a `cmd/noto/`.

`main` monta o `context` raiz com `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`, passa adiante, e converte erro em código de saída — um único ponto de saída do processo.

**Decisão: o `context` é a única forma de encerramento.** Nada de canal de shutdown próprio. `serve` fecha o `http.Server` com `Shutdown(ctx)` quando o contexto cai; `worker` usa `select` entre `ctx.Done()` e o `time.Ticker`.

## 3. Configuração

`internal/platform/config` expõe `Load() (Config, error)`: lê o ambiente, aplica defaults, valida, e retorna erro agregando **todas** as variáveis problemáticas — não a primeira. Diagnosticar três variáveis erradas em três reinícios é atrito gratuito.

**Decisão: sem biblioteca de config (Viper, envconfig, koanf).** Dez variáveis, uma fonte. `os.Getenv` mais validação explícita é menos código do que a configuração da biblioteca, e mantém a mensagem de erro sob nosso controle — que é justamente a parte que importa aqui. *Alternativa rejeitada:* `caarlos0/env` com tags de struct; economiza ~30 linhas e custa uma dependência e mensagens de erro genéricas.

**Decisão: redação de segredos via `LogValue()`.** `Config` implementa `slog.LogValuer` devolvendo os campos sensíveis como `"[REDACTED]"`. Isso torna a proteção do PRD §12 estrutural: quem logar a config, de onde for, nunca vaza o token — não depende de lembrar de não logar.

`DATABASE_URL` é redigido com a senha removida da URL, não a URL inteira, porque o host e o database são informação de diagnóstico legítima.

**Decisão: no scaffolding, a única variável incondicionalmente obrigatória é `DATABASE_URL`.** `TELEGRAM_WEBHOOK_SECRET` é obrigatória apenas sob `TELEGRAM_TRANSPORT=webhook`. Todo o resto é opcional e pode vir vazio — inclusive `TELEGRAM_BOT_TOKEN`.

O raciocínio: uma variável é obrigatória quando **algum código desta entrega falha sem ela**. `migrate` precisa de `DATABASE_URL`; nada aqui precisa do token do Telegram, porque não existe adapter de Telegram ainda. Exigi-lo agora tornaria impossível a coisa mais básica que alguém faz ao clonar o repositório — `cp .env.example .env && docker compose up` — sem antes ir ao BotFather criar um bot. Isso trocaria uma falha clara no M1 por uma barreira arbitrária no M0. *Consequência assumida:* a regra endurece no M1, e quem introduzir o adapter de Telegram precisa lembrar de mover o token para obrigatório — está registrado como risco no `tasks.md`.

Defaults: `LOG_LEVEL=info`, `TELEGRAM_TRANSPORT=polling`, `DEFAULT_TIMEZONE=America/Sao_Paulo`, `OLLAMA_HOST=https://ollama.com` — os mesmos já escritos em `.env.example`, para que o arquivo e o código não divirjam.

## 4. Teste de fronteira arquitetural

Este é o único artefato desta entrega com lógica de verdade, e o ADR 0001 o nomeia como a mitigação da erosão da fronteira.

**Decisão: `golang.org/x/tools/go/packages` com `NeedImports|NeedDeps`, não `go list` via `exec`, nem uma ferramenta de terceiros.**

- `go list -deps` por `os/exec` funciona, mas devolve uma lista achatada: quando falha, ela diz *que* há uma violação, não *por qual caminho*. Numa base com dezenas de pacotes, isso vira caça manual.
- `go/packages` dá o grafo, então o teste reporta a cadeia `core/resolve → core/x → adapters/postgres → pgx`. Um teste de fronteira que não aponta o caminho é um teste que alguém vai silenciar em vez de corrigir.
- *Alternativa rejeitada:* `go-arch-lint`/`depguard` — ferramenta externa com arquivo de configuração próprio, para uma regra de quatro linhas que precisa estar dentro de `go test ./...` para valer no CI.

A lista de proibições é declarada como prefixos de caminho de import: `github.com/jackc/pgx`, `github.com/go-telegram/bot`, qualquer `.../ollama`, e `github.com/duvrdx/noto/internal/adapters`. A stdlib é sempre permitida.

O teste vive em `internal/core/boundary_test.go` — dentro de `core`, porque é uma propriedade de `core`, e é onde alguém que mexe em `core` vai vê-lo.

## 5. Compose

Quatro serviços: `postgres`, `lgtm`, `serve`, `worker`. `serve` e `worker` compartilham a mesma imagem construída do mesmo `Dockerfile`, diferindo só no comando — é o ADR 0001 tornado visível na topologia.

**Decisão: `depends_on` com `condition: service_healthy` no Postgres.** Sem isso, `serve` e `worker` reiniciam em laço no primeiro `up` até o banco aceitar conexões, e o log inicial vira ruído que ensina o desenvolvedor a ignorar log.

**Decisão: as migrações não rodam automaticamente no `up`.** `make migrate` é um passo explícito. Migração automática em start é conveniente em dev e perigosa em produção, e o ADR 0008 quer a mesma topologia nos dois ambientes — então o comportamento seguro é o que vale nos dois.

`postgres` usa um volume nomeado para os dados; `lgtm` não, porque telemetria de desenvolvimento não precisa sobreviver a um `down -v`.

**Decisão: portas publicadas no host — Postgres em `5432`, `serve` em `8080`.** Publicar o Postgres permite `psql` direto e testes rodando fora de contêiner, que é como a maior parte do desenvolvimento acontece. Se colidir com um Postgres já instalado na máquina, o ajuste é um override local do Compose, não uma mudança do plano.

**Decisão: `DATABASE_URL` e `OTEL_EXPORTER_OTLP_ENDPOINT` são sobrescritos dentro do `docker-compose.yml`, e o `.env.example` continua em `localhost`.** Os dois endereços têm valor diferente conforme quem lê: para um binário rodando no host, `localhost:5432` e `localhost:4317` estão certos, porque as portas estão publicadas; para um contêiner dentro da rede do Compose, `localhost` é o próprio contêiner, e o certo é `postgres` e `lgtm`.

A alternativa — apontar o `.env.example` para `postgres`/`lgtm` — quebraria o `go run ./cmd/noto` fora de contêiner, que é o caminho de desenvolvimento mais frequente. A outra alternativa, dois arquivos de exemplo, duplica dez variáveis para diferenciar duas. Sobrescrever só as duas divergentes, no lugar onde a divergência nasce, é o menor delta.

```yaml
serve:
  env_file: [.env]
  environment:
    DATABASE_URL: postgres://noto:noto@postgres:5432/noto?sslmode=disable
    OTEL_EXPORTER_OTLP_ENDPOINT: http://lgtm:4317
```

*Consequência assumida:* a URL do banco existe em dois lugares e pode divergir em silêncio. Mitigante barato e obrigatório: um comentário no `docker-compose.yml` explicando por que o override existe.

**Decisão: `.env.example` com comentários em linha própria.** Hoje quatro variáveis têm comentário depois do valor (`TELEGRAM_TRANSPORT`, `TELEGRAM_WEBHOOK_SECRET`, `OLLAMA_HOST`, `OLLAMA_API_KEY`). Nem o `env_file` do Compose nem `set -a; . arquivo` tratam isso como comentário: ambos incorporam o texto ao valor, e `TELEGRAM_TRANSPORT` passaria a valer `polling          # polling (dev) | webhook (prod)`, reprovando a validação da §3. Os comentários sobem para a linha de cima; nenhum valor muda.

## 6. Migrações e sqlc

`migrations/00001_enable_pg_trgm.sql` com `-- +goose Up` / `-- +goose Down`. Só a extensão.

**Decisão: `goose` embutido como biblioteca no subcomando `migrate`, com as migrações em `embed.FS`.** O ADR 0001 diz "embutível no binário"; embutir agora evita que o binário de produção dependa de um arquivo no disco do host ou de um binário `goose` instalado lá. `make migrate` chama `noto migrate`, então há um caminho só, não dois.

`sqlc.yaml`: engine `postgresql`, `sql_package: pgx/v5`, schema apontando para `migrations/`, queries para `internal/adapters/postgres/queries/`, saída em `internal/adapters/postgres/db/`. O `sqlc` v1.31.1 **recusa zero queries** (`no queries contained in paths`, mesmo com um `.sql` só de comentário), então o scaffolding inclui a query real `Ping` (`SELECT 1`), que servirá depois a um readiness check. O código gerado é versionado em `internal/adapters/postgres/db/` e a prova de que a configuração está certa é `sqlc generate` sair com sucesso e `go build ./...` compilar o resultado. O `sqlc` ignora o bloco `-- +goose Down` do schema.

## 7. Makefile

Alvos finos, sem lógica: cada um é uma linha de comando que um humano poderia digitar. Um Makefile que vira programa é um programa sem testes. A única exceção é o export de `PATH` no topo do arquivo (§1.1), que é infraestrutura de invocação, não lógica.

```
build         go build -o bin/noto ./cmd/noto
test          go test -race ./...
lint          go vet ./... && staticcheck ./...
migrate       go run ./cmd/noto migrate
eval          eval-parse + eval-resolve
eval-parse    go test -tags eval ./evals/parse/...
eval-resolve  go test -tags eval ./evals/resolve/...
```

`eval-resolve` precisa sair com **código 0** com zero cenários, porque o CI (§8) o executa em todo PR desde já.

`eval-strategy.md` §7 pede `make eval CATEGORY=rel` e `make eval MODEL=...`; ambos são repassados como variáveis de ambiente para o `go test`, e o consumo delas é do M2 — aqui só o encanamento existe.

## 8. CI

Um workflow, um job, na ordem `vet → staticcheck → test -race → make eval-resolve`, com cache de módulos. `-race` é inegociável (`testing.md` §5).

**Decisão: `make eval-resolve` entra no CI já nesta entrega, mesmo sendo um no-op.** `testing.md` §5 lista quatro passos em todo PR, não três; `eval-resolve` entra porque é determinístico, roda em segundos e cobre o risco mais grave do produto (acertar o item errado numa ação destrutiva). Encanar o passo agora, verde e vazio, significa que o primeiro cenário de resolução do M2 já nasce sendo verificado — em vez de depender de alguém lembrar de editar o workflow no meio de outra entrega. *Alternativa rejeitada:* adiar para quando houver cenários; é exatamente o tipo de passo que se esquece.

`eval-parse` continua fora do CI padrão: chama serviço externo pago e só roda quando muda prompt ou modelo.

**Nota:** os testes de caso de uso e de integração usam testcontainers (`testing.md` §3), que precisa de Docker no runner. Nenhum teste desta entrega usa banco, então o CI passa hoje sem isso — mas o runner escolhido precisa ter Docker disponível, ou o M1 trava. O workflow é escrito assumindo `ubuntu-latest`, que tem.

## 9. Inventário de arquivos

**Criados**

```
go.mod, go.sum
Dockerfile
docker-compose.yml
Makefile
sqlc.yaml
.github/workflows/ci.yml
migrations/00001_enable_pg_trgm.sql
migrations/embed.go               (go:embed não alcança diretórios pais; ver task 5.2)
internal/adapters/postgres/queries/ping.sql
internal/adapters/postgres/db/    (código gerado pelo sqlc, versionado)
cmd/noto/main.go
cmd/noto/serve.go
cmd/noto/worker.go
cmd/noto/migrate.go
internal/platform/config/config.go
internal/platform/logging/logging.go
internal/platform/migrations/embed.go
internal/platform/http/          (doc.go — servidor e middlewares são M1)
internal/core/boundary_test.go
internal/core/{item,revision,resolve,timex,ports}/doc.go
internal/app/doc.go
internal/adapters/{telegram,ollama,postgres,otel}/doc.go
evals/parse/, evals/resolve/     (placeholders com a build tag eval)
```

**Modificados**

```
README.md        seção "Rodando localmente" com os comandos reais, portas 8080/5432 e a nota do PATH; M1 continua ⬜
.env.example     comentários inline movidos para linha própria; nenhum valor alterado
.gitignore       só se a estrutura final exigir (bin/ e vendor/ já estão cobertos)
```

**Já existentes, não tocados:** `docs/**`.

## 10. O que deliberadamente não existe ainda

Cada um destes é uma omissão decidida, não um esquecimento:

- **Nenhuma tabela do PRD §6.2.** Vêm com o código que as usa.
- **Nenhuma interface em `core/ports`.** Uma interface escrita antes de ter um implementador e um consumidor é um chute sobre a forma da dependência. O ADR 0001 nomeia os portos; o M1 os escreve.
- **Nenhum cliente HTTP de Telegram ou Ollama.**
- **Nenhuma instrumentação OTel além da variável de ambiente lida.** O `lgtm` sobe e fica ocioso; é o M1 que começa a exportar.
- **`internal/app` vazio.**

Pacotes vazios existem com um `doc.go` porque um diretório sem arquivo `.go` não é um pacote, não compila, e não é verificável pelo teste de fronteira.
