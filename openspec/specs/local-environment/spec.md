# local-environment Specification

## Purpose
TBD - created by archiving change add-project-scaffolding. Update Purpose after archive.

## Requirements

### Requirement: `docker compose up` sobe o ambiente completo

O repositório SHALL conter um `docker-compose.yml` que sobe, com um único comando e sem passo manual adicional, os serviços: `postgres:16` com a extensão `pg_trgm` disponível, `grafana/otel-lgtm`, e os processos `serve` e `worker` do mesmo binário `noto` (PRD §8.3, §10; `architecture.md` §2; ADR 0008). Produção e desenvolvimento SHALL compartilhar a mesma topologia, diferindo apenas em transporte e segredos.

#### Scenario: Ambiente sobe do zero

- **Given** um clone limpo do repositório com `.env` preenchido a partir de `.env.example`
- **When** `docker compose up` é executado
- **Then** os quatro serviços ficam de pé, o Postgres aceita conexões e `GET /healthz` do `serve` responde `200`

#### Scenario: `pg_trgm` está disponível

- **Given** o serviço de Postgres do Compose em execução com as migrações aplicadas
- **When** a extensão `pg_trgm` é consultada em `pg_extension`
- **Then** ela está instalada

#### Scenario: Portas publicadas no host

- **Given** o Compose em execução
- **When** as portas publicadas são inspecionadas
- **Then** o Postgres está acessível no host em `5432` e o `serve` em `8080`

#### Scenario: Serve e worker são contêineres distintos

- **Given** o Compose em execução
- **When** os contêineres são listados
- **Then** `serve` e `worker` aparecem como contêineres separados do mesmo binário, sem nenhum link de rede direto entre si

### Requirement: Endereços de serviço são sobrescritos dentro da rede do Compose

Os serviços `serve` e `worker` SHALL ler o `.env` do desenvolvedor e, **dentro do `docker-compose.yml`**, sobrescrever `DATABASE_URL` para apontar ao host `postgres` e `OTEL_EXPORTER_OTLP_ENDPOINT` para apontar ao host `lgtm` — os nomes de serviço resolvíveis na rede do Compose. O `.env.example` SHALL continuar apontando para `localhost`, que é o valor correto para executar o binário fora de contêiner; esta mudança SHALL NOT alterá-lo por causa do Compose.

#### Scenario: Serve dentro do Compose alcança o Postgres

- **Given** um `.env` copiado de `.env.example`, com `DATABASE_URL` apontando para `localhost`
- **When** `docker compose up -d` sobe o ambiente
- **Then** o contêiner `serve` conecta ao Postgres do Compose pelo host `postgres`, sem que o `.env` tenha sido editado

#### Scenario: Telemetria aponta para o LGTM do Compose

- **Given** o Compose em execução
- **When** a configuração efetiva do contêiner `worker` é inspecionada
- **Then** `OTEL_EXPORTER_OTLP_ENDPOINT` aponta para o host `lgtm`, não para `localhost`

#### Scenario: Binário fora de contêiner usa localhost

- **Given** o mesmo `.env` e nenhum override do Compose aplicado
- **When** `noto serve` é executado diretamente no host
- **Then** ele usa os endereços `localhost` do `.env`, alcançando as portas publicadas pelo Compose

### Requirement: Makefile como interface de desenvolvimento

O repositório SHALL conter um `Makefile` com os alvos `build`, `test`, `lint`, `migrate` e `eval`, mais `eval-parse` e `eval-resolve` separados (`eval-strategy.md` §7). `test` SHALL rodar `go test -race ./...` e SHALL NOT incluir os evals; os evals SHALL ficar atrás da build tag `eval` (`testing.md` §5).

#### Scenario: Build produz o binário

- **Given** o toolchain Go instalado
- **When** `make build` roda
- **Then** o binário `noto` é produzido em `bin/`, caminho já coberto pelo `.gitignore`

#### Scenario: Test não chama serviço externo

- **Given** nenhuma credencial de Ollama no ambiente
- **When** `make test` roda
- **Then** a suíte passa sem nenhuma chamada de rede a serviço de inferência

#### Scenario: Lint roda vet e staticcheck

- **Given** o toolchain e o `staticcheck` instalados
- **When** `make lint` roda
- **Then** `go vet ./...` e `staticcheck ./...` são executados e o alvo falha se qualquer um reportar problema

#### Scenario: Alvos funcionam com o Go fora do PATH do shell

- **Given** um shell em que `/usr/local/go/bin` não está no `PATH`
- **When** `make build` roda
- **Then** o alvo encontra o toolchain por um export interno ao `Makefile` e conclui sem erro, sem exigir alteração do perfil do shell do usuário

#### Scenario: Eval de resolução roda separado do eval de extração

- **Given** o repositório provisionado
- **When** `make eval-resolve` roda
- **Then** apenas os testes com a tag `eval` do eixo de resolução são executados, sem chamada a LLM, e o alvo encerra com código zero mesmo não havendo nenhum cenário ainda

### Requirement: CI verifica todo PR

O repositório SHALL conter um pipeline de CI que roda, em todo PR, `go vet ./...`, `staticcheck ./...`, `go test -race ./...` e `make eval-resolve` (`testing.md` §5). A flag `-race` SHALL ser obrigatória. `make eval-resolve` SHALL rodar em todo PR por ser determinístico e cobrir o risco mais grave do produto; enquanto não houver cenários, o alvo SHALL encerrar com código zero. `eval-parse` SHALL NOT rodar no CI padrão, por chamar serviço externo pago.

#### Scenario: PR verde

- **Given** um PR sem violações
- **When** o CI roda
- **Then** os quatro passos passam e o PR fica apto a ser mesclado

#### Scenario: Corrida de dados reprova

- **Given** um PR que introduz uma corrida de dados detectável
- **When** `go test -race ./...` roda no CI
- **Then** o job falha

#### Scenario: Eval de resolução é no-op verde no scaffolding

- **Given** o repositório nesta entrega, sem nenhum cenário de resolução
- **When** o CI executa `make eval-resolve`
- **Then** o passo encerra com código zero e o pipeline prossegue

#### Scenario: Eval de extração fica fora do CI padrão

- **Given** o workflow de CI do repositório
- **When** seus passos são inspecionados
- **Then** nenhum deles executa `make eval-parse`

### Requirement: CI reprova vulnerabilidades conhecidas chamadas pelo código

O pipeline de CI SHALL executar `govulncheck ./...` em todo PR, com a versão da ferramenta fixada, e SHALL falhar quando o código do repositório chamar uma vulnerabilidade conhecida da biblioteca padrão ou de uma dependência. O CI SHALL usar o último patch da versão `major.minor` declarada no `go.mod`, para que a biblioteca padrão avaliada esteja corrigida. Avisos sobre módulos que o código não chama SHALL NOT reprovar o pipeline.

#### Scenario: Código afetado por vulnerabilidade reprova

- **Given** um PR cuja árvore de dependências inclui uma versão vulnerável que o código chama
- **When** o CI executa `govulncheck ./...`
- **Then** o passo falha e o PR não pode ser mesclado

#### Scenario: Árvore limpa passa

- **Given** um PR sem chamadas a vulnerabilidades conhecidas
- **When** o CI executa `govulncheck ./...`
- **Then** o passo encerra com código zero e o pipeline prossegue

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
