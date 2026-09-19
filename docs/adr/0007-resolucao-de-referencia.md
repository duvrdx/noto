# ADR 0007 — Resolução de referência determinística

- **Status:** Aceito
- **Data:** 2026-09-19

## Contexto

Com `complete`, `cancel` e `update` por linguagem natural no MVP, surge um problema que não existia quando o produto só criava itens: **"cancela a reunião de amanhã" precisa virar um `item_id`.**

Isso é um problema distinto de extração de entidades, e mais perigoso. Extrair errado produz um item torto, visível, corrigível. Resolver errado **altera o compromisso errado** — e a operação acerta um dado que o usuário considerava seguro.

Agrava-se pelo fato de o sistema agir sem confirmação prévia ([ADR 0006](./0006-confirmacao-seletiva-e-desfazer.md)). A margem de segurança precisa estar dentro do resolver.

## Decisão

**A resolução é feita por código determinístico com acesso ao banco. O LLM descreve a referência; ele nunca escolhe o alvo.**

### 1. O modelo devolve um descritor, nunca um identificador

```json
{
  "intent": "cancel",
  "reference": {
    "terms": ["reunião", "Fulano"],
    "entity_type": "meeting",
    "time_hint": { "kind": "day", "date": "2026-09-20" },
    "status_hint": "pending"
  }
}
```

Não existe caminho pelo qual o modelo emita um `item_id`. Não é validação: é ausência do campo no schema. Um identificador alucinado numa operação de mutação é uma classe de falha que este desenho torna inexpressável.

### 2. Candidatos por filtro barato

Itens do usuário, com status compatível com a ação (não se conclui o que já está concluído), dentro de uma janela temporal: `time_hint` presente → ±1 dia em torno dela; ausente → próximos 14 dias mais os últimos 2.

### 3. Pontuação explícita

```
score = 0.50 × similaridade_de_título   (pg_trgm)
      + 0.30 × proximidade_temporal
      + 0.20 × recência_de_criação
```

### 4. Decisão por limiar **e** margem

| Condição | Ação |
|---|---|
| `top_score ≥ 0.60` **e** `top_score − segundo ≥ 0.15` | age |
| candidatos existem, sem margem | pergunta |
| nenhum candidato | informa e oferece `/pendentes` |

A **margem** é o mecanismo central, mais que o limiar absoluto. Duas reuniões parecidas produzem scores altos e próximos: pontuação alta sozinha diria "aja"; a margem diz "pergunte". É exatamente o caso em que errar o alvo é mais provável.

### 5. A política vive no domínio

Pontuação e decisão ficam em `internal/core/resolve`. O adapter Postgres só fornece candidatos e similaridade textual. Os limiares são calibráveis e testáveis sem banco.

### 6. Toda decisão é registrada

A tabela `resolutions` grava referência, número de candidatos, `top_score`, margem, item escolhido e desfecho — em toda resolução, inclusive nas que não agiram.

## Alternativas consideradas

**Mandar os candidatos ao LLM e pedir que escolha.** Rejeitado por três razões independentes, qualquer uma suficiente: (a) coloca um componente não determinístico no comando de uma operação de mutação, que é precisamente onde ele não deve estar; (b) acrescenta uma segunda chamada de inferência à latência e ao custo de toda mutação; (c) produz uma escolha sem score nem margem — não haveria como distinguir "óbvio" de "chute", e portanto não haveria critério para perguntar.

**Busca textual pura (`ILIKE` / full-text).** Ignora o sinal temporal, que é justamente o mais forte na fala real: "a reunião de amanhã" tem uma dica de data mais discriminante que a palavra "reunião", que provavelmente aparece em vários itens.

**Sempre perguntar qual item.** Seguro e contrário ao produto. Reintroduziria pela porta dos fundos o atrito que o ADR 0006 removeu pela da frente, e no caminho mais comum — quando há um único item plausível — a pergunta é pura cerimônia.

**Resolver por ordinal ou identificador visível** ("cancela o item 3"). Exigiria o usuário consultar antes de agir, transformando uma frase em uma navegação. É a interface que o Noto existe para substituir.

**Embeddings e busca vetorial.** Rejeitado por desproporção: dezenas de candidatos numa janela de duas semanas não justificam pipeline de embedding, armazenamento vetorial e mais uma dependência de inferência. `pg_trgm` resolve com uma extensão nativa. Se o eval mostrar que a similaridade léxica é o fator limitante, reavaliar — com dado.

## Consequências

**Aceitas como positivas**

- O pior modo de falha do produto fica sob controle de código auditável, testável e ajustável — não de um prompt.
- `score` e `margem` dão à decisão de perguntar um critério numérico, calibrável contra `resolutions` e o eval.
- Sem chamada extra de LLM: a resolução é uma query indexada, com orçamento de p95 < 150 ms (§9 do PRD).
- A política é testável sem banco e sem rede.

**Aceitas como custo**

- **Os pesos e limiares são chute inicial.** São arbitrários até a calibragem do M6. É por isso que `resolutions` registra score e margem desde o primeiro dia — sem esse registro, os números permaneceriam chute para sempre.
- **A similaridade léxica falha em paráfrase.** "Já paguei a luz" contra um item chamado "Conta de energia" tende a não casar. O sinal temporal ajuda, e o fallback é uma pergunta — não uma ação errada. É a direção correta de falha, mas é uma limitação real.
- **Mais um componente com estado para observar.** Compensado por `resolutions` ser também o instrumento de eval do M5.
