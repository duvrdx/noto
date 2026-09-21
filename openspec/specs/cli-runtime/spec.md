# cli-runtime Specification

## Purpose
TBD - created by archiving change add-project-scaffolding. Update Purpose after archive.

## Requirements

### Requirement: Binário único com três modos de execução

O projeto SHALL produzir um único binário `noto` que expõe exatamente os subcomandos `serve`, `worker` e `migrate`, conforme ADR 0001 e PRD §8.1. Um subcomando desconhecido, ou a ausência de subcomando, SHALL fazer o processo escrever o uso na saída de erro e encerrar com código diferente de zero.

#### Scenario: Subcomando válido inicia

- **Given** o binário `noto` compilado
- **When** ele é invocado como `noto serve`, `noto worker` ou `noto migrate`
- **Then** o processo inicia sem erro de uso e registra em log qual modo assumiu

#### Scenario: Subcomando desconhecido é recusado

- **Given** o binário `noto` compilado
- **When** ele é invocado como `noto frobnicate` ou sem nenhum argumento
- **Then** o uso é escrito em stderr e o processo encerra com código de saída diferente de zero

### Requirement: `serve` e `worker` são processos independentes

Os modos `serve` e `worker` SHALL ser processos distintos que não se comunicam diretamente entre si; toda coordenação SHALL passar pelo PostgreSQL (ADR 0001, `architecture.md` §2). Nenhum dos dois SHALL depender da presença do outro para iniciar.

#### Scenario: Worker sobe sem a API

- **Given** nenhum processo `noto serve` em execução
- **When** `noto worker` é iniciado
- **Then** ele inicia normalmente e executa seu laço de tick

#### Scenario: API sobe sem o worker

- **Given** nenhum processo `noto worker` em execução
- **When** `noto serve` é iniciado
- **Then** ele passa a atender requisições HTTP normalmente

### Requirement: Endpoint de saúde

O modo `serve` SHALL expor `GET /healthz` usando `net/http` com o `ServeMux` da stdlib (ADR 0001 — sem framework web), respondendo `200 OK` quando o processo está apto a atender.

#### Scenario: Healthz responde

- **Given** `noto serve` em execução na porta configurada
- **When** um `GET /healthz` é feito
- **Then** a resposta tem status `200`

### Requirement: Encerramento gracioso por sinal

Todo modo de execução SHALL encerrar de forma graciosa ao receber `SIGINT` ou `SIGTERM`: o `context` raiz é cancelado, os laços em andamento param, e o processo sai com código zero. Isso é pré-requisito do reinício sem perda descrito no PRD §9 (Durabilidade) e em `architecture.md` §9.

#### Scenario: SIGTERM encerra o serve

- **Given** `noto serve` em execução
- **When** o processo recebe `SIGTERM`
- **Then** o servidor HTTP para de aceitar conexões, o processo encerra com código zero, e o log registra o encerramento

#### Scenario: SIGINT encerra o worker

- **Given** `noto worker` em execução no meio de um intervalo de tick
- **When** o processo recebe `SIGINT`
- **Then** o laço de tick termina e o processo encerra com código zero
