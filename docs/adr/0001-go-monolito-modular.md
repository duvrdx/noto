# ADR 0001 — Go, monólito modular e binário único

- **Status:** Aceito
- **Data:** 2026-09-19

## Contexto

O Noto precisa de: um endpoint HTTP que responda ao webhook do Telegram dentro do timeout, um processo de fundo que dispare lembretes no horário, e um caminho de evolução para arquitetura distribuída — que é um dos objetivos de aprendizado declarados do projeto.

A tentação natural é começar com serviços separados, broker de mensagens e deploys independentes, porque é assim que o sistema vai parecer no fim.

## Decisão

**Monólito modular em Go, com binário único e múltiplos modos de execução.**

```
noto serve     # HTTP
noto worker    # scheduler + despachante de outbox
noto migrate
```

Organização interna em portas e adaptadores:

- `internal/core` — domínio puro, sem dependência de infraestrutura
- `internal/app` — casos de uso
- `internal/adapters` — Telegram, Ollama, Postgres, OTel
- `internal/platform` — config, logging, HTTP, migrations

`serve` e `worker` rodam como processos distintos no Compose e **não se comunicam diretamente** — toda coordenação é via Postgres.

Bibliotecas: `net/http` com o `ServeMux` do Go 1.22+, `pgx` + `sqlc`, `goose`, `go-telegram/bot`, `log/slog`, OpenTelemetry. Sem framework web, sem ORM.

## Alternativas consideradas

**Microsserviços desde o dia 1** (API, parser, scheduler e bot separados, com broker). Rejeitado: o custo operacional — rede, descoberta, versionamento de contrato, tracing distribuído, orquestração local — chega imediatamente, enquanto o benefício depende de escala que este produto não terá tão cedo. Pior, dificultaria a única coisa que importa no início: iterar rápido no prompt e no fluxo de conversa.

**Serverless.** Rejeitado por três razões: cold start conflita com o orçamento de latência do eco; o scheduler baseado em `SKIP LOCKED` (ADR 0003) precisa de processo vivo; e elimina justamente o aprendizado de runtime que motiva o projeto.

**Framework web (Echo, Gin, Fiber) e ORM (GORM).** Rejeitados. O roteamento com método e path params entrou na stdlib no Go 1.22 — o framework não resolveria nenhum problema real. Um ORM esconderia exatamente o SQL que este projeto quer exercitar (`FOR UPDATE SKIP LOCKED`, outbox transacional). `sqlc` dá tipagem sem esconder a query.

## Consequências

**Aceitas como positivas**

- Uma transação cobre operações que em serviços separados exigiriam saga ou compensação.
- Refatorar fronteiras de módulo é uma mudança de pacote, não uma migração de contrato entre serviços.
- O caminho para distribuído está pré-pago: `serve` e `worker` já são processos independentes coordenando por banco, e o worker já é seguro para rodar em múltiplas réplicas. Separá-los em deploys distintos não exige mudança de código.

**Aceitas como custo**

- Deploy é acoplado: uma mudança no parser reinicia também o scheduler. Aceitável na escala atual.
- A fronteira arquitetural depende de disciplina, e disciplina erode. **Mitigação:** teste de arquitetura no CI que falha se `internal/core/...` importar `pgx`, `telegram` ou `ollama`. O Princípio 4 do PRD vira verificação automatizada, não boa intenção.
- A validação real da fronteira só virá com o segundo canal, na v2.

## Atualização (2026-09-19) — piso do módulo

O `ServeMux` com método e path params (Go 1.22+) continua sendo a razão de não haver framework web, mas **o piso da diretiva `go` do módulo não é mais 1.22: é 1.26** (`go 1.26.0`). O Noto é uma aplicação, não uma biblioteca: quem o compila usa o toolchain do projeto, então um piso baixo não beneficia ninguém. E ele custava caro: fixar `go 1.22` obrigava a travar `pgx v5.7.4` e `x/text v0.21.0`, versões com vulnerabilidades que o código chama (GO-2026-5004, SQL injection no pgx, corrigida na 5.9.2; GO-2026-5970, laço infinito no x/text, corrigida na 0.39), cujas correções exigem Go 1.25/1.26. Decisão do usuário: subir o piso e atualizar as dependências (`pgx v5.11.0`, `goose v3.28.0`, `x/text v0.42.0`).
