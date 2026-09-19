# Tasks — scaffolding inicial

## Convenções desta entrega

**Commits.** Conventional Commits com escopo (`feat(config):`, `chore(build):`), corpo em português. Decidido pelo usuário — ver *Decisões tomadas* item 1. Toda mensagem de commit termina com a linha de atribuição:

```
Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
```

Os textos de commit citados nas tasks abaixo são a **primeira linha**; a linha de atribuição é obrigatória em todos eles e não é repetida em cada task.

**Toolchain Go fora do `PATH`.** O Go 1.27.1 está instalado em `/usr/local/go/bin`, mas **não está no `PATH` do shell do usuário**. Antes de qualquer comando `go`, `sqlc`, `goose` ou `staticcheck`, quem executa precisa rodar:

```sh
export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH
```

Isso vale para toda task das seções 2 em diante. O `~/.zshrc` do usuário **não** deve ser alterado por nenhuma task deste plano; em vez disso, o `Makefile` faz esse export internamente (task 6.3), de modo que `make <alvo>` funcione num shell limpo.

**Ordem é dependência real, não preferência.** Nada da seção 2 em diante roda antes da seção 1 estar completa.

## 0. Higiene de histórico

- [x] 0.1 Executar a estratégia de primeiro commit decidida (*Decisões tomadas* item 2, opção **b**): **dois commits separados**, nesta ordem — (1) o M0, contendo `README.md`, `docs/` inteiro, `.gitignore` e `.env.example`; (2) a adoção do OpenSpec, contendo `openspec/` (incluindo `openspec/config.yaml` com `--language pt-BR`) e `.claude/` (6 skills, 6 commands), que é commitado no repositório por decisão do usuário. O scaffolding em si vem depois, nos commits das tasks seguintes. Verificação: `git log --oneline` mostra exatamente esses dois commits, nessa ordem, antes de qualquer commit de código, e `git status` não lista mais nenhum dos caminhos acima. Commits: `docs(m0): PRD, arquitetura, estratégias de teste e eval, e ADRs` e `chore(openspec): adota OpenSpec como convenção de mudança`. (owner: human) *Executada pelo spongebob por delegação do usuário.*

## 1. Pré-requisitos de ambiente

Nenhuma task abaixo produz código. Todas instalam ou verificam software na máquina do usuário, fora do repositório.

- [x] 1.1 Instalar o toolchain Go no WSL. **Satisfeita**: `go version` reporta **Go 1.27.1**, com o binário em `/usr/local/go/bin/go` — acima do piso 1.22 do ADR 0001 (`ServeMux` do 1.22+). Ressalva registrada: o diretório **não está no `PATH`**; ver *Convenções desta entrega*. Sem commit. (owner: human)
- [x] 1.2 Habilitar a integração do Docker Desktop com esta distro WSL. **Satisfeita**: `docker info` responde com **Docker 28.5.1** e `docker compose version` reporta **Compose v2.40**. Sem commit. (owner: human)
- [x] 1.3 Instalar as ferramentas de desenvolvimento ausentes: `sqlc`, `goose` e `staticcheck`, via `go install` (operação mecânica, sem julgamento). **Depende de 1.1**, já satisfeita. Antes de rodar, exportar o `PATH` conforme *Convenções desta entrega* — os binários caem em `$HOME/go/bin`, que também precisa estar no `PATH`. Verificação: `sqlc version`, `goose -version` e `staticcheck -version` respondem, num shell onde só o export acima foi feito. Sem commit; se as versões forem fixadas em `tools.go`/`go.mod`, isso entra na task 2.1. (owner: agent)

## 2. Módulo e árvore de pacotes

- [x] 2.1 Criar `go.mod` para o módulo `github.com/duvrdx/noto` (caminho derivado do remoto `origin`, não inventado) com a diretiva **`go 1.22`** — o piso do ADR 0001, decidido pelo usuário (*Decisões tomadas* item 3). O toolchain 1.27.1 instalado compila isso sem problema; a diretiva **não** deve ser elevada para a versão do toolchain. Arquivos: `go.mod`. Sem teste próprio. Verificação: `go build ./...` (trivialmente verde, sem pacotes ainda) e `grep '^go 1.22' go.mod`. Commit: `chore(build): inicializa módulo Go`. (owner: agent) *Atualização (2026-09-19): o piso do módulo subiu de `go 1.22` para `go 1.26.0` por decisão do usuário, para corrigir GO-2026-5004 (pgx, SQL injection) e GO-2026-5970 (x/text, laço infinito) — ver o commit `chore(deps): eleva piso do Go para 1.26 e corrige vulnerabilidades`.*
- [x] 2.2 Criar a árvore de pacotes de `design.md` §9 — `internal/core/{item,revision,resolve,timex,ports}`, `internal/app`, `internal/adapters/{telegram,ollama,postgres,otel}`, `internal/platform/{config,logging,http,migrations}` — cada diretório com um `doc.go` declarando o pacote e uma linha dizendo o que ele vai conter, conforme PRD §8.2. Sem teste próprio. Verificação: `go build ./...` compila todos os pacotes; `go vet ./...` limpo. Commit: `chore(layout): cria árvore de pacotes do PRD §8.2`. (owner: agent)

## 3. Configuração e logging

Regra que atravessa toda esta seção: **no scaffolding, a única variável obrigatória é `DATABASE_URL`.** `TELEGRAM_BOT_TOKEN` vazio **não** derruba o `serve` — ele só se torna obrigatório no M1, quando existe um adapter de Telegram que o usa. As demais variáveis têm default ou são condicionalmente obrigatórias (`TELEGRAM_WEBHOOK_SECRET` só quando `TELEGRAM_TRANSPORT=webhook`). Ver spec `configuration`.

- [x] 3.1 Escrever primeiro o teste de tabela de `internal/platform/config/config_test.go` cobrindo os cenários da spec `configuration`: carga completa; `DATABASE_URL` ausente (**único** caso de obrigatória faltando); **`.env.example` recém-copiado, com `TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEBHOOK_SECRET`, `OLLAMA_API_KEY` e `OLLAMA_MODEL` vazios, mais um `DATABASE_URL` válido → carga bem-sucedida**; `TELEGRAM_TRANSPORT` inválido; `webhook` sem `TELEGRAM_WEBHOOK_SECRET`; `DEFAULT_TIMEZONE` inválido; e a agregação de múltiplos erros numa mensagem só. O teste deve falhar por `config.Load` não existir. Verificação: `go test ./internal/platform/config/` falha na compilação — o vermelho esperado. Commit: `test(config): cenários de carga e validação de ambiente`. (owner: agent)
- [x] 3.2 Implementar `config.Load` em `internal/platform/config/config.go` com os defaults de `.env.example` e validação agregada, tratando **apenas** `DATABASE_URL` como incondicionalmente obrigatória e `TELEGRAM_WEBHOOK_SECRET` como obrigatória sob `TELEGRAM_TRANSPORT=webhook`. Campos vazios opcionais são carregados como string vazia, sem erro e sem aviso fatal. Verificação: `go test ./internal/platform/config/` passa, incluindo o caso do `.env` recém-copiado. Commit: `feat(config): carrega e valida configuração do ambiente`. (owner: agent)
- [x] 3.3 Escrever o teste de redação em `config_test.go`: `Config` implementa `slog.LogValuer` e a saída não contém `TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEBHOOK_SECRET`, `OLLAMA_API_KEY` nem a senha de `DATABASE_URL` (PRD §12). Implementar `LogValue()` em seguida. Verificação: `go test ./internal/platform/config/` passa com o novo caso. Commit: `feat(config): reda segredos na saída de log`. (owner: agent)
- [x] 3.4 Escrever o teste de `internal/platform/logging/logging_test.go` verificando handler JSON e filtragem por `LOG_LEVEL` (uma linha `debug` some com `LOG_LEVEL=info`), depois implementar `logging.New`. Arquivos: `internal/platform/logging/{logging.go,logging_test.go}`. Verificação: `go test ./internal/platform/logging/` passa. Commit: `feat(logging): logger slog em JSON com nível configurável`. (owner: agent)

## 4. Binário e subcomandos

- [x] 4.1 Escrever o teste de despacho de subcomando em `cmd/noto/main_test.go`: subcomando desconhecido e ausência de subcomando produzem uso em stderr e código de saída diferente de zero; `serve`/`worker`/`migrate` são reconhecidos. Testar a função de despacho, não o `main`. Verificação: `go test ./cmd/noto/` falha antes da implementação. Commit: `test(cli): despacho de subcomandos`. (owner: agent)
- [x] 4.2 Implementar `cmd/noto/main.go`: `signal.NotifyContext` para `SIGINT`/`SIGTERM`, carga de config, montagem do logger, despacho, e um ponto único de saída traduzindo erro em código. Verificação: `go test ./cmd/noto/` passa; `go run ./cmd/noto` imprime o uso e sai com código ≠ 0. Commit: `feat(cli): entrypoint com despacho e encerramento por sinal`. (owner: agent)
- [x] 4.3 Escrever o teste de `GET /healthz` respondendo `200` via `httptest`, depois implementar `cmd/noto/serve.go` com `net/http` + `ServeMux` e `Shutdown(ctx)` no cancelamento do contexto. A porta HTTP do `serve` é **8080** (*Decisões tomadas* item 6). Verificação: `go test ./cmd/noto/` passa; `noto serve` local responde `curl -sf localhost:8080/healthz`, e `Ctrl-C` encerra com código zero. Commit: `feat(serve): servidor HTTP com healthz e shutdown gracioso`. (owner: agent)
- [x] 4.4 Escrever o teste de que o laço de tick do worker retorna ao cancelamento do contexto (relógio/ticker curto, sem `sleep` de segundos), depois implementar `cmd/noto/worker.go` com um tick vazio que só loga. Nenhuma lógica de fila — é M4. Verificação: `go test -race ./cmd/noto/` passa. Commit: `feat(worker): laço de tick com encerramento por contexto`. (owner: agent)

## 5. Migrações e acesso a dados

- [x] 5.1 Criar `migrations/00001_enable_pg_trgm.sql` com `-- +goose Up` criando a extensão e `-- +goose Down` removendo-a. Nenhuma tabela do PRD §6.2. Verificação: inspeção do arquivo; o comportamento contra um banco real é verificado na task **6.4**. Commit: `feat(db): migração inicial habilita pg_trgm`. (owner: agent)
- [x] 5.2 Implementar `internal/platform/migrations/embed.go` com `embed.FS` sobre `migrations/` e `cmd/noto/migrate.go` usando `goose` como biblioteca contra `DATABASE_URL`. Verificação: `go build ./...` compila e `noto migrate` roda contra um banco inexistente falhando com erro claro de conexão (não com panic). Commit: `feat(migrate): subcomando migrate com migrações embutidas`. (owner: agent) *Nota de execução: `//go:embed` não alcança diretórios pais, então o `embed.FS` vive em `migrations/embed.go` (pacote `migrations`, ao lado dos `.sql`) e `internal/platform/migrations` é quem o usa para aplicar (`Up`). Dependências fixadas em versões compatíveis com `go 1.22`: `goose/v3 v3.24.1` e `pgx/v5 v5.7.4` (as versões mais novas exigem Go 1.23 a 1.26).* *Atualização (2026-09-19): as versões `goose v3.24.1` e `pgx v5.7.4`, fixadas para caber em `go 1.22`, foram substituídas por `goose v3.28.0` e `pgx v5.11.0` quando o piso subiu para `go 1.26.0` (vulnerabilidades GO-2026-5004 e GO-2026-5970).*
- [x] 5.3 Criar `sqlc.yaml` (engine `postgresql`, `sql_package: pgx/v5`, schema em `migrations/`, queries em `internal/adapters/postgres/queries/`, saída em `internal/adapters/postgres/db/`, com comentário no topo dizendo como regenerar) e a query real `queries/ping.sql` (`-- name: Ping :one` / `SELECT 1;`). Verificação: `sqlc generate` encerra com sucesso, o código gerado (versionado em `internal/adapters/postgres/db/`) passa em `go build ./...` e `go vet ./...` contra o `pgx` fixado, e o `sqlc` ignora o bloco `-- +goose Down`. Commit: `chore(db): configura sqlc com query de readiness`. (owner: agent) *Nota: a redação original previa "zero queries" e um `.gitkeep`; o `sqlc` v1.31.1 recusa isso (`no queries contained in paths`, mesmo com um `.sql` só de comentário). Corrigido para incluir a query `Ping`, que servirá a um readiness check; contrato ajustado na spec `database-migrations`.*

## 6. Ambiente local

- [x] 6.1 Reescrever `.env.example` movendo **todo comentário inline para uma linha própria acima da variável**, afetando `TELEGRAM_TRANSPORT`, `TELEGRAM_WEBHOOK_SECRET`, `OLLAMA_HOST` e `OLLAMA_API_KEY`. Comentário depois do valor é lido como parte do valor por `env_file` do Compose e por `set -a; . arquivo` do shell — hoje `TELEGRAM_TRANSPORT` valeria `polling          # polling (dev) | webhook (prod)` e reprovaria a validação da task 3.2. **Os valores em si não mudam**: `DATABASE_URL` e `OTEL_EXPORTER_OTLP_ENDPOINT` continuam apontando para `localhost` (o override para a rede do Compose é da task 6.2). Arquivos: `.env.example`. Verificação: `set -a; . ./.env.example; set +a` num shell limpo termina sem erro e `echo "$TELEGRAM_TRANSPORT"` imprime exatamente `polling`; e, depois da task 6.2, `cp .env.example .env && docker compose config` valida sem avisos de valor. Commit: `fix(env): comentários em linha própria no .env.example`. (owner: agent)
- [x] 6.2 Escrever o `Dockerfile` (build multi-stage do binário único) e o `docker-compose.yml` com `postgres:16` (volume nomeado, healthcheck, **publicado no host em `5432:5432`**), `grafana/otel-lgtm`, e os serviços `serve` (**publicado em `8080:8080`**) e `worker` da mesma imagem com comandos distintos e `depends_on: service_healthy` no banco. `serve` e `worker` leem `.env` via `env_file` e **sobrescrevem no próprio Compose** `DATABASE_URL` (host `postgres`) e `OTEL_EXPORTER_OTLP_ENDPOINT` (host `lgtm`), porque dentro da rede do Compose `localhost` é o próprio contêiner; o `.env.example` **não** é alterado por esta task. Sem execução automática de migrações. Verificação: `docker compose config` valida e mostra os dois overrides resolvidos para `postgres` e `lgtm`; `docker compose up -d` deixa os quatro serviços de pé e `curl -sf localhost:8080/healthz` responde `200`. Commit: `feat(compose): ambiente local com postgres, lgtm, serve e worker`. (owner: agent) *Nota (2026-09-19): as portas são publicadas apenas em `127.0.0.1` (`"127.0.0.1:5432:5432"` etc.), não em `0.0.0.0`, porque as credenciais são de desenvolvimento e o Grafana usa o login padrão; continua sendo "publicado no host". Também foram publicadas `3000`, `4317` e `4318` do `lgtm`.*
- [x] 6.3 Escrever o `Makefile` com `build`, `test`, `lint`, `migrate`, `eval`, `eval-parse`, `eval-resolve`, repassando `CATEGORY` e `MODEL` aos alvos de eval (`eval-strategy.md` §7). O Makefile **exporta internamente** `PATH=/usr/local/go/bin:$(HOME)/go/bin:$(PATH)`, para que os alvos funcionem num shell onde o Go não está no `PATH` (ver *Convenções desta entrega*); o `~/.zshrc` do usuário não é tocado. Verificação: num shell **sem** o export manual, `make build` produz `bin/noto`; `make test` roda `go test -race ./...` verde; `make lint` roda `go vet` e `staticcheck` sem achados. Commit: `chore(make): alvos de build, teste, lint, migração e eval`. (owner: agent)
- [x] 6.4 Verificar o ciclo de migração contra o Postgres do Compose: aplicar em banco limpo, conferir `pg_trgm` em `pg_extension`, confirmar que nenhuma tabela de domínio existe, e reexecutar para provar que é inerte (cenários da spec `database-migrations`). **Depende de 5.2, 6.2 e 6.3** — precisa do subcomando, do banco de pé e do alvo `make migrate`. Verificação: `make migrate` duas vezes seguidas, ambas com sucesso, mais `SELECT extname FROM pg_extension WHERE extname='pg_trgm'` retornando uma linha e `\dt` sem nenhuma tabela do PRD §6.2. Commit: nenhum se nada mudar; senão, `fix(migrate): ...`. (owner: agent)
- [x] 6.5 Criar os pacotes placeholder de eval `evals/parse/` e `evals/resolve/` atrás da build tag `eval`, com um teste que hoje não assere nada além de o pacote compilar. `make eval-resolve` precisa **sair com código 0** com zero cenários, porque a task 8.1 o coloca no CI de todo PR. Verificação: `go test ./...` **não** os executa; `make eval-resolve` os executa e encerra com código 0; `make eval-parse` também. Commit: `chore(eval): encanamento dos alvos de eval atrás da tag de build`. (owner: agent)

## 7. Fronteira arquitetural

- [x] 7.1 Escrever `internal/core/boundary_test.go` usando `golang.org/x/tools/go/packages` para carregar `./internal/core/...` com `NeedImports|NeedDeps` e falhar quando um import proibido (`pgx`, cliente de Telegram, cliente de Ollama, `internal/adapters/...`) aparecer direta ou transitivamente, reportando a cadeia completa. Cobre os quatro cenários da spec `architecture-boundary`. Verificação: `go test ./internal/core/...` passa na árvore limpa. Commit: `test(core): fronteira arquitetural do domínio`. (owner: agent)
- [x] 7.2 Provar que o teste **falha** quando deve: introduzir temporariamente um import de `pgx` num pacote de `core`, rodar o teste, confirmar a falha e a cadeia reportada, e reverter. Um teste de fronteira nunca exercitado no vermelho é decoração. Verificação: com o import plantado, `go test ./internal/core/...` falha nomeando o pacote e o import; após `git checkout`, passa. Sem commit. (owner: agent)

## 8. CI

- [ ] 8.1 Escrever `.github/workflows/ci.yml` rodando, em push e PR, `go vet ./...`, `staticcheck ./...`, `go test -race ./...` **e `make eval-resolve`** — os quatro passos que `testing.md` §5 exige em todo PR —, com cache de módulos e **sem** `eval-parse` (que chama serviço externo pago e só roda quando muda prompt ou modelo). O passo `make eval-resolve` é hoje um no-op verde (task 6.5) e existe para que o eixo determinístico de resolução já esteja encanado no CI quando os cenários chegarem no M2. **Depende de 6.5.** Verificação: os quatro comandos passam localmente na mesma ordem; o workflow fica verde no primeiro push com PR aberto; `grep -c eval-parse .github/workflows/ci.yml` retorna `0`. Commit: `ci: vet, staticcheck, testes com -race e eval-resolve em todo PR`. (owner: agent)
- [ ] 8.2 Atualizar a seção "Rodando localmente" do `README.md` com os comandos reais (`cp .env.example .env`, `docker compose up`, `make migrate`, `make test`), citando as portas `8080` (serve) e `5432` (Postgres) e a nota do `PATH` do Go, e manter M1 como ⬜ — o scaffolding não é o *walking skeleton*. Verificação: leitura humana; todo comando citado foi executado com sucesso em alguma task anterior. Commit: `docs(readme): instruções de execução local`. (owner: human)

## 9. Verificação final

- [ ] 9.1 Rodar o critério de pronto completo, do zero, num clone limpo: `make build` · `docker compose up -d` · `make migrate` · `make test` · `make lint` · `make eval-resolve`. Todos verdes. Verificação: os seis comandos, nessa ordem, sem intervenção manual além de copiar `.env.example` para `.env` e preencher `DATABASE_URL` se o default não servir. Sem commit. (owner: agent)

---

## Decisões tomadas

Registradas aqui porque estavam em aberto e o usuário decidiu; não são mais risco, são premissa.

1. **Convenção de commit.** Conventional Commits com escopo, corpo em português, e a linha `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>` ao fim de **toda** mensagem de commit. Ver *Convenções desta entrega*.

2. **Primeiro commit — opção (b).** M0 (`README.md`, `docs/`, `.gitignore`, `.env.example`) e adoção do OpenSpec (`openspec/` e `.claude/`) em **commits separados**, nessa ordem, antes de qualquer commit de código. Task 0.1. Permanece `owner: human` porque é a inauguração do histórico do repositório, um ato de curadoria que nenhum teste verifica.

3. **Diretiva `go` do `go.mod`: `go 1.22`.** O piso do ADR 0001. O toolchain instalado (1.27.1) é compatível com essa diretiva; ela **não** sobe para a versão do toolchain, para não fechar a porta a quem tem Go mais antigo. Task 2.1. *Atualização (2026-09-19): o piso do módulo subiu de `go 1.22` para `go 1.26.0` por decisão do usuário, para corrigir GO-2026-5004 (pgx, SQL injection) e GO-2026-5970 (x/text, laço infinito) — ver o commit `chore(deps): eleva piso do Go para 1.26 e corrige vulnerabilidades`.*

4. **`--language pt-BR` no `openspec/config.yaml`: mantido.**

5. **`.claude/` do projeto é commitado** — vai junto da adoção do OpenSpec, no segundo commit da task 0.1.

6. **Portas do Compose.** Postgres publicado no host em `5432`; `serve` publicado em `8080`. *(Atualização 2026-09-19: publicadas apenas no loopback, `127.0.0.1`.)* Tasks 4.3 e 6.2. Se colidir com um Postgres já instalado na máquina, o ajuste é local (`docker compose` com override), não uma mudança do plano.

7. **Variáveis obrigatórias no scaffolding: só `DATABASE_URL`.** `TELEGRAM_BOT_TOKEN` vazio não impede o `serve` de subir; ele passa a ser obrigatório no M1, quando o adapter de Telegram existir. Seção 3 e spec `configuration`.

8. **`make eval-resolve` entra no CI de todo PR**, como manda `testing.md` §5, ainda que hoje seja um alvo verde sem cenários. Tasks 6.5 e 8.1.

## Questões em aberto / riscos

1. **Toolchain Go fora do `PATH`.** Go 1.27.1 existe em `/usr/local/go/bin`, mas um shell novo do usuário não o encontra. O plano contorna isso em dois lugares (nota para quem executa, export dentro do `Makefile`) e **deliberadamente não altera o `~/.zshrc`**. O risco residual é um comando `go` avulso, fora do `make`, falhar com "command not found" e ser interpretado como Go ausente — o que já aconteceu uma vez na redação anterior desta proposta.

2. **Runner de CI precisa de Docker.** Nenhum teste desta entrega usa banco, então o CI passa hoje sem Docker no runner. A partir do M1, `testing.md` §3 exige testcontainers, e um runner sem Docker trava o marco seguinte. O plano assume `ubuntu-latest` do GitHub Actions, que atende.

3. **Divergência entre `.env.example` e o Compose.** `.env.example` aponta para `localhost` (correto para rodar o binário fora de contêiner) e o Compose sobrescreve para `postgres`/`lgtm` (correto dentro da rede). São dois ambientes legítimos, mas a duplicação é um lugar onde as duas metades podem divergir em silêncio. Mitigante barato: um comentário no `docker-compose.yml` dizendo *por que* o override existe. Não há decisão pendente aqui — é um risco a vigiar.

4. **`TELEGRAM_BOT_TOKEN` opcional agora, obrigatório no M1.** A regra muda de marco para marco, e a spec `configuration` registra a versão do scaffolding. Quem implementar o M1 precisa lembrar de endurecer a validação; se esquecer, o `serve` do M1 sobe com um adapter de Telegram sem token e falha só na primeira chamada de rede.

5. **Risco de o scaffolding envelhecer.** Placeholders que não são preenchidos logo viram arquivos que ninguém lembra por que existem. O mitigante é o M1 começar imediatamente depois; se ele for adiado por semanas, vale reduzir o escopo desta entrega em vez de deixar dez `doc.go` órfãos.
