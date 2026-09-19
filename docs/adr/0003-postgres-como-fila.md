# ADR 0003 — Postgres como fila, com outbox transacional

- **Status:** Aceito
- **Data:** 2026-09-19

## Contexto

O Noto precisa de duas garantias de entrega:

1. **Lembretes** devem disparar no horário, sobreviver a reinício, e nunca ser entregues em duplicidade mesmo com múltiplos workers concorrentes.
2. **Mensagens de saída** ao Telegram precisam ser consistentes com o estado do domínio: não pode existir "lembrete marcado como enviado, mensagem nunca enviada" nem o inverso.

O volume esperado do MVP é de dezenas a centenas de mensagens por dia.

## Decisão

**Sem broker de mensagens. O Postgres é a fila.**

### Reivindicação de trabalho

```sql
UPDATE reminders r
SET status = 'claimed',
    locked_until = now() + interval '2 minutes',
    attempts = attempts + 1
WHERE r.id IN (
    SELECT id FROM reminders
    WHERE status = 'pending' AND remind_at <= now()
    ORDER BY remind_at
    FOR UPDATE SKIP LOCKED
    LIMIT 50
)
RETURNING r.*;
```

`SKIP LOCKED` permite N workers concorrentes sem coordenação externa e sem entrega dupla. Registro com status `claimed` e `locked_until` no passado é trabalho órfão de worker que morreu — recuperado pelo tick seguinte.

### Outbox transacional

Toda mensagem ao usuário é inserida na tabela `outbox` **dentro da mesma transação** que altera o estado do domínio. Um despachante separado lê a outbox e envia, com backoff em caso de erro.

`outbox.dedup_key` é `UNIQUE`: uma reentrega após timeout ambíguo não produz mensagem duplicada no chat.

Semântica resultante: **at-least-once no envio, com deduplicação na borda** — efeito exactly-once do ponto de vista do usuário.

## Alternativas consideradas

**Redis + asynq, ou RabbitMQ.** Rejeitado. Traz um segundo sistema com estado para operar, monitorar e manter consistente com o Postgres — e reintroduz exatamente o problema que a outbox resolve: escrever no banco e publicar no broker não são atômicos, então seria preciso... uma outbox. O broker adiciona a peça sem remover o problema, num volume que o Postgres atende com folga.

**`pg_cron` ou `pg_notify`.** `pg_cron` não modela retry, tentativas nem estado por item. `pg_notify` não é durável: uma notificação emitida enquanto nenhum listener está conectado é perdida — inaceitável para lembretes.

**`time.Timer` em memória.** Rejeitado de imediato: reiniciar o processo perderia todo lembrete agendado. Viola o requisito de durabilidade do §9 do PRD.

**Envio direto no handler, sem outbox.** Simples e errado: se o `sendMessage` falha após o commit, o lembrete fica marcado como enviado e o usuário nunca é avisado. É a falha mais grave que este produto pode ter.

## Consequências

**Aceitas como positivas**

- Uma única peça com estado para operar, fazer backup e restaurar.
- Estado da fila é inspecionável com SQL — depurar "por que esse lembrete não chegou" é uma query, não arqueologia em broker.
- Exercita garantias de entrega, locking concorrente e o padrão outbox de forma explícita, que é objetivo de aprendizado do projeto.

**Aceitas como custo**

- *Polling*, não *push*: há latência de até um tick. Com tick de 10 s, cabe folgadamente no alvo de p95 < 30 s (§9 do PRD).
- Não escala para milhares de mensagens por segundo. Muito além do horizonte deste produto; se chegar lá, a interface `ports.Queue` permite trocar a implementação sem tocar nos casos de uso.
- O tick gera carga constante no banco, mesmo ocioso. Irrelevante nesta escala.
