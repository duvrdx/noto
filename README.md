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

✅ **M1 pronto: o bot já conversa.** Uma mensagem de texto enviada em chat privado ao bot no Telegram é gravada no Postgres e respondida com o mesmo texto (eco). Ainda não há interpretação: o parser é o M2.

| Marco | Status |
|---|---|
| M0 · PRD, ADRs, arquitetura | ✅ |
| M1 · Walking skeleton | ✅ |
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

**Pré-requisitos:** Docker com Compose v2, Go 1.26+ e um bot do Telegram. Para criar o bot, converse com o [`@BotFather`](https://t.me/BotFather), envie `/newbot` e guarde o token. Para `make lint`, o `staticcheck` precisa estar instalado (`go install honnef.co/go/tools/cmd/staticcheck@2026.2.1`).

```sh
cp .env.example .env            # depois edite o .env e preencha TELEGRAM_BOT_TOKEN
docker compose up -d --build    # postgres, grafana/otel-lgtm, serve e worker
make migrate                    # cria pg_trgm, users e messages (idempotente)
curl -sf localhost:8080/healthz # 200 OK
docker compose logs -f serve    # início do servidor HTTP e do polling
```

Agora mande uma mensagem de texto ao bot, numa conversa **privada**: ele responde com o mesmo texto, e a linha aparece em `messages`:

```sh
docker compose exec postgres psql -U noto -d noto \
  -c "SELECT telegram_update_id, chat_id, raw_text FROM messages ORDER BY received_at DESC LIMIT 3;"
```

Foto, sticker e mensagens de grupo são ignorados, com um log do `update_id` e do motivo.

**`TELEGRAM_BOT_TOKEN` é obrigatório.** `serve`, `worker` e `migrate` recusam iniciar sem ele, e o `serve` valida o token no Telegram na subida (precisa de rede). Ele fica só no `.env` local, que é gitignored: o `.env.example` é versionado e mantém o valor vazio.

**Verificação e encerramento:**

```sh
make test                       # go test -race ./...; exige Docker (Postgres via testcontainers)
go test -short ./...            # atalho sem Docker: pula os testes que precisam dele
make lint                       # go vet + staticcheck
make eval-resolve               # eixo determinístico do eval (hoje sem cenários)
docker compose down             # preserva o volume do Postgres; `down -v` apaga os dados
```

**Um bot por ambiente.** Polling e webhook no mesmo bot conflitam (o `getUpdates` recebe `409`), e duas instâncias consultando o mesmo bot também. Se o log do `serve` mostrar `conflito no getUpdates`, há um webhook ativo ou outra instância usando o mesmo token. O webhook ainda não é suportado: `TELEGRAM_TRANSPORT=webhook` faz o `serve` recusar iniciar.

**Portas.** Todas publicadas **apenas em `127.0.0.1`**, porque o Compose usa credenciais de desenvolvimento (`noto`/`noto`): `8080` (serve), `5432` (Postgres), `3000` (Grafana), `4317` e `4318` (OTLP). Em produção a exposição é por proxy reverso.

**Go fora do `PATH`.** O `Makefile` exporta `/usr/local/go/bin` e `$HOME/go/bin` internamente, então os alvos `make` funcionam mesmo num shell onde `go` não é encontrado. Comandos `go` avulsos precisam dele no seu `PATH`.

**Transporte do Telegram.** Em desenvolvimento o bot usa **long polling** — sem túnel, sem domínio, sem HTTPS (`TELEGRAM_TRANSPORT=polling`). Ver [ADR 0008](docs/adr/0008-transporte-telegram-e-deploy.md).

**Segredos.** `TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEBHOOK_SECRET` e `OLLAMA_API_KEY` ficam só no `.env` local. Nunca em código, log ou commit.

## Privacidade

O texto das mensagens é enviado a um serviço de inferência de terceiros (Ollama Cloud) para interpretação. A arquitetura mantém a portabilidade para inferência **local** como requisito — ver [ADR 0002](docs/adr/0002-ollama-como-parser.md).
