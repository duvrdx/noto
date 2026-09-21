# Noto

**Assistente pessoal conversacional para tarefas, compromissos e lembretes.**

Você manda uma frase. O Noto entende, estrutura e te lembra na hora certa.

> "Amanhã tenho uma reunião com o Fulano sobre o orçamento."
>
> → 📅 *Reunião com Fulano* — sáb, 20/09 às 14:00 · `[Desfazer]`

E quando acabar, uma frase também:

> "Já falei com o Fulano."
>
> → ✅ *Concluído: Reunião com Fulano* · `[Desfazer]`

Sem abrir app, sem date picker, sem preencher cinco campos — e sem "tem certeza?". O Noto age e deixa você desfazer.

---

## Estado

🚧 **Base pronta, produto ainda não.** A documentação (M0) e o scaffolding estão feitos: o binário sobe, valida configuração e responde `/healthz`, com Postgres, migrações, CI e uma fronteira arquitetural verificada por teste. **O bot ainda não responde no Telegram** — isso é o M1.

| Marco | Status |
|---|---|
| M0 · PRD, ADRs, arquitetura | ✅ |
| M1 · Walking skeleton | ⬜ |
| M2 · Parser (todas as intenções) + eval | ⬜ |
| M3 · Criação, revisões e desfazer | ⬜ |
| M4 · Scheduler e lembretes | ⬜ |
| M5 · Resolver + concluir/cancelar/editar por NL | ⬜ |
| M6 · Observabilidade, calibragem e deploy | ⬜ |

## Stack

Go · PostgreSQL · Ollama (Cloud → local) · Telegram · OpenTelemetry · Docker Compose

## Documentação

| Documento | Conteúdo |
|---|---|
| [PRD](docs/PRD.md) | Visão, escopo, modelo de domínio, métricas, riscos |
| [Arquitetura](docs/architecture.md) | Contêineres, componentes, fluxos, recuperação de falha |
| [Estratégia de eval](docs/eval-strategy.md) | Como extração e resolução de referência são medidas |
| [Estratégia de teste](docs/testing.md) | Pirâmide, dublês, casos inegociáveis, CI |

### Decisões de arquitetura

- [0001 · Go e monólito modular](docs/adr/0001-go-monolito-modular.md)
- [0002 · Ollama como parser](docs/adr/0002-ollama-como-parser.md)
- [0003 · Postgres como fila](docs/adr/0003-postgres-como-fila.md)
- [0004 · Confirmação sempre na v1](docs/adr/0004-confirmacao-sempre-no-v1.md) *(substituído pelo 0006)*
- [0005 · Modelo temporal e fusos](docs/adr/0005-modelo-temporal-e-timezone.md)
- [0006 · Confirmação seletiva e desfazer](docs/adr/0006-confirmacao-seletiva-e-desfazer.md)
- [0007 · Resolução de referência determinística](docs/adr/0007-resolucao-de-referencia.md)
- [0008 · Transporte do Telegram e alvo de deploy](docs/adr/0008-transporte-telegram-e-deploy.md)

## Rodando localmente

> **O que você verá hoje:** o ambiente sobe e o `serve` responde `GET /healthz`. O bot **não responde no Telegram ainda** — o adapter chega no M1. `TELEGRAM_BOT_TOKEN` é opcional por enquanto e passa a ser obrigatório no M1.

**Pré-requisitos:** Docker com Compose v2 e Go 1.26+. Para `make lint`, o `staticcheck` precisa estar instalado (`go install honnef.co/go/tools/cmd/staticcheck@2026.2.1`).

```sh
cp .env.example .env            # único passo manual; .env é gitignored
docker compose up -d --build    # postgres, grafana/otel-lgtm, serve e worker
make migrate                    # habilita a extensão pg_trgm (idempotente)
curl -sf localhost:8080/healthz # 200 OK
```

Verificação e encerramento:

```sh
make test                       # go test -race ./...
make lint                       # go vet + staticcheck
make eval-resolve               # eixo determinístico do eval (hoje sem cenários)
docker compose down             # preserva o volume do Postgres; `down -v` apaga os dados
```

**Portas.** Todas publicadas **apenas em `127.0.0.1`**, porque o Compose usa credenciais de desenvolvimento (`noto`/`noto`): `8080` (serve), `5432` (Postgres), `3000` (Grafana), `4317` e `4318` (OTLP). Em produção a exposição é por proxy reverso.

**Go fora do `PATH`.** O `Makefile` exporta `/usr/local/go/bin` e `$HOME/go/bin` internamente, então os alvos `make` funcionam mesmo num shell onde `go` não é encontrado. Comandos `go` avulsos precisam dele no seu `PATH`.

**Transporte do Telegram.** Em desenvolvimento o bot usa **long polling** — sem túnel, sem domínio, sem HTTPS (`TELEGRAM_TRANSPORT=polling`). O webhook é de produção. Ver [ADR 0008](docs/adr/0008-transporte-telegram-e-deploy.md).

**Segredos.** `TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEBHOOK_SECRET` e `OLLAMA_API_KEY` ficam só no `.env` local. O `.env.example` é versionado e mantém esses valores vazios.

## Privacidade

O texto das mensagens é enviado a um serviço de inferência de terceiros (Ollama Cloud) para interpretação. A arquitetura mantém a portabilidade para inferência **local** como requisito — ver [ADR 0002](docs/adr/0002-ollama-como-parser.md).
