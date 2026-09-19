# Noto — Estratégia de teste

Complementa a [estratégia de avaliação](./eval-strategy.md), que cobre os dois componentes não determinísticos. Este documento cobre o resto: o código comum, que é a maior parte.

O princípio é um só: **testes rápidos e determinísticos em maioria esmagadora, testes lentos apenas onde o valor exige**.

---

## 1. Pirâmide

| Camada | O que cobre | Velocidade | Roda |
|---|---|---|---|
| **Unitário** | `core/` — invariantes, transições, pontuação do resolver, aritmética temporal | ms | todo salvamento |
| **Caso de uso** | `app/` com repositório real e `Parser` falso | segundos | todo PR |
| **Integração** | `adapters/postgres` contra Postgres real | dezenas de segundos | todo PR |
| **Contrato de canal** | adapter Telegram, os dois transportes | segundos | todo PR |
| **Ponta a ponta** | Compose de pé, fluxo completo | minutos | antes do deploy |
| **Eval** | Extração e resolução | minutos / segundos | ver `eval-strategy.md` |

`core/` não tem dependência de infraestrutura, então seus testes não têm setup. É o principal benefício prático da fronteira do [ADR 0001](./adr/0001-go-monolito-modular.md) — e a razão de a pontuação do resolver morar em `core/resolve` e não no adapter Postgres: calibrar limiares é um teste de tabela em milissegundos, não uma query.

---

## 2. Dublês

Três, e apenas três:

| Interface | Dublê | Por quê |
|---|---|---|
| `ports.Parser` | falso com respostas fixas por entrada | Determinismo. Nenhum teste de caso de uso chama LLM |
| `ports.Clock` | relógio controlável | Horário de verão, virada de dia e de ano sem depender da máquina |
| `ports.Notifier` | coletor em memória | Verifica *o que* seria enviado, sem rede |

Repositórios **não** são dublados. Testes de caso de uso rodam contra Postgres real via testcontainers, porque a maior parte da lógica que importa nesses casos é transacional — e um repositório falso testaria a fidelidade do falso, não a do sistema. Ver §8.5 do PRD: mutação, snapshot e mensagem de saída vivem na mesma transação, e é exatamente isso que precisa ser verificado.

---

## 3. Banco nos testes

`testcontainers-go` sobe um Postgres com `pg_trgm`, migrações aplicadas uma vez por execução. Cada teste roda numa transação com rollback ao final — isolamento sem custo de recriar schema.

```go
func TestCompleteItem(t *testing.T) {
    db := testdb.New(t)          // container compartilhado, TX com rollback
    clk := clock.Fixed("2026-09-19T10:00:00-03:00")
    // ...
}
```

---

## 4. Casos que precisam existir

Lista não exaustiva, mas estes são inegociáveis — cada um corresponde a um risco nomeado no PRD:

**Reversibilidade** ([ADR 0006](./adr/0006-confirmacao-seletiva-e-desfazer.md))
- toda mutação grava `item_revisions` com `before` correto
- desfazer restaura exatamente o estado anterior
- desfazer sobre revisão que não é a mais recente **não** sobrescreve, e informa
- desfazer é idempotente sob duplo toque no botão

**Concorrência** ([ADR 0003](./adr/0003-postgres-como-fila.md))
- dois workers simultâneos não entregam o mesmo lembrete duas vezes
- worker morto com `locked_until` expirado tem o trabalho retomado
- `dedup_key` impede mensagem duplicada no retry

**Idempotência** ([ADR 0008](./adr/0008-transporte-telegram-e-deploy.md))
- update reentregue não executa a ação duas vezes — crítico, já que a ação não tem confirmação prévia

**Resolução** ([ADR 0007](./adr/0007-resolucao-de-referencia.md))
- margem insuficiente sempre pergunta, nunca age
- `callback_data` forjado com índice fora do intervalo é rejeitado
- callback de outro usuário é rejeitada

**Tempo** ([ADR 0005](./adr/0005-modelo-temporal-e-timezone.md))
- "amanhã" na virada do dia, no fuso do usuário e não no do servidor
- item criado em torno de mudança de offset mantém o instante correto
- `/hoje` usa a janela do usuário, não a do processo

**Fronteira arquitetural**
- teste que falha se `internal/core/...` importar `pgx`, `telegram` ou `ollama`

O container de teste roda em UTC deliberadamente, para que qualquer suposição vazada sobre o fuso do processo apareça logo.

---

## 5. CI

```
go vet · staticcheck · go test -race ./...   →  todo PR
make eval-resolve                            →  todo PR (determinístico, segundos)
make eval-parse                              →  só quando muda prompt ou modelo
```

`-race` é obrigatório: o worker é concorrente por projeto, e é ali que corrida de dados apareceria.

`eval-parse` fica fora do CI padrão porque chama serviço externo e custa dinheiro. `eval-resolve` entra porque é determinístico e cobre o risco mais grave do produto — acertar o item errado numa ação destrutiva.

---

## 6. O que não se testa

- **Formatação exata das mensagens.** Muda toda semana; um teste aqui quebra por design, não por defeito. Testa-se que a mensagem certa foi *escolhida*, não como ela lê.
- **A API do Telegram.** Não é nossa. O adapter tem contrato testado; o serviço, não.
- **A qualidade do modelo em teste unitário.** É o que o eval faz, com instrumento próprio e critério próprio.
