# Noto — Estratégia de avaliação

O Noto tem dois componentes não determinísticos no caminho crítico, e ambos precisam ser medidos continuamente:

1. **Extração** — a frase vira um objeto estruturado (LLM).
2. **Resolução de referência** — a descrição vira um item específico do banco (determinístico, mas com limiares arbitrários até serem calibrados).

Errar a extração produz um item torto, que o usuário vê. **Errar a resolução altera o compromisso errado** — e como o sistema age sem confirmação prévia ([ADR 0006](./adr/0006-confirmacao-seletiva-e-desfazer.md)), esse é o modo de falha mais grave do produto.

Os dois eixos têm conjuntos, métricas e gates separados.

---

## Parte I — Extração

### 1. Por que isso existe antes do código

Três decisões futuras dependem de um número confiável de qualidade:

1. **Mudar o prompt** — sem eval, toda mudança é aposta e regressões passam despercebidas até um usuário perder um compromisso.
2. **Trocar de modelo** — o catálogo do Ollama Cloud muda. Sem eval, "este parece melhor" é impressão.
3. **Migrar para inferência local** — a decisão de maior peso do roadmap ([ADR 0002](./adr/0002-ollama-como-parser.md), §12 do [PRD](./PRD.md)). Só pode ser tomada comparando local e cloud no mesmo conjunto.

### 2. O golden set

~120 enunciados reais em PT-BR, cada um com a saída esperada. Não são frases inventadas em inglês traduzido — a fala brasileira real é o objeto de teste.

#### Categorias obrigatórias

| Categoria | Exemplos |
|---|---|
| Data relativa simples | "amanhã", "depois de amanhã", "segunda que vem" |
| Data relativa ambígua | "sexta" (esta ou a próxima?), "dia 5" |
| Hora coloquial | "meio-dia", "fim da tarde", "de manhã cedo", "2 da tarde" |
| Sem informação temporal | "preciso comprar leite" |
| Só data, sem hora | "entregar o relatório segunda" |
| Duração / intervalo | "reunião das 14h às 16h" |
| Entidade nomeada | "reunião com o Dr. Silva no consultório" |
| Múltiplos itens na frase | "amanhã tenho dentista e preciso pagar o IPVA" → espera `intent: multiple` |
| Erros de digitação e abreviação | "amanha 14h rnuiao com fulano" |
| Fronteira entre tipos | "ligar pro dentista às 15h" (task ou event?) |
| **Intenção de conclusão** | "já paguei a conta", "terminei o relatório", "feito" |
| **Intenção de cancelamento** | "cancela a reunião de amanhã", "não vou mais no dentista" |
| **Intenção de edição** | "muda o dentista pra quinta às 10h", "adia a reunião em 1h" |
| **Ambiguidade create vs. complete** | "reunião com Fulano" (criar? marcar como feita?) |
| **Lembrete implícito vs. explícito** | "me lembra às 14h" vs. "reunião às 14h" — §7.6 do PRD |
| Virada de fuso / horário de verão | casos em torno de mudanças de offset |
| Hostil / vazio | só emoji, texto muito longo, tentativa de prompt injection |

As cinco categorias em negrito entraram na v0.2 com a inclusão de mutação por linguagem natural. A penúltima é a mais delicada: uma frase sem verbo pode ser criação ou conclusão, e errar aí faz o sistema criar um item duplicado ou fechar um que estava aberto.

A categoria de múltiplos itens espera `intent: multiple` com os fragmentos detectados, **não** uma lista de itens estruturados: o schema devolve um objeto por chamada, e a limitação é declarada ao usuário em vez de silenciada (§7.2 do PRD). Um caso de teste que esperasse dois itens estruturados testaria um comportamento que o sistema não promete.

Cada caso vive em `evals/cases/*.yaml`:

```yaml
- id: rel-001
  input: "Amanhã tenho uma reunião com Fulano sobre orçamento"
  now: "2026-09-19T10:00:00-03:00"
  timezone: "America/Sao_Paulo"
  expect:
    intent: create
    entity_type: meeting
    title_contains: ["reunião", "Fulano"]
    starts_at_date: "2026-09-20"
    missing: ["starts_at_time"]

- id: mut-014
  input: "cancela a reunião de amanhã"
  now: "2026-09-19T10:00:00-03:00"
  timezone: "America/Sao_Paulo"
  expect:
    intent: cancel
    reference:
      terms_contain: ["reunião"]
      time_hint_date: "2026-09-20"
```

Note `title_contains` em vez de `title` exato: exigir string idêntica mede aderência ao prompt, não qualidade de extração, e gera falhas ruidosas que ninguém investiga.

### 3. Como se mede

**Acurácia por campo, não match exato.** Um resultado que acerta intenção, tipo e data mas escreve "Reunião c/ Fulano" em vez de "Reunião com Fulano" é sucesso para o usuário.

| Campo | Critério |
|---|---|
| `intent` | igualdade — **o campo mais importante**, ver abaixo |
| `entity_type` | igualdade |
| `title` | contém os termos-chave esperados |
| `starts_at` | tolerância de ±1 minuto |
| `reference.terms` | contém os termos-chave |
| `reference.time_hint` | igualdade de data |
| `missing` | igualdade de conjunto |
| schema | JSON parseou e validou |

`intent` é reportado separadamente como **matriz de confusão**, não como acurácia agregada. Nem todo erro de intenção custa o mesmo: confundir `create` com `query` gera uma listagem inesperada; confundir `create` com `cancel` cancela algo que o usuário queria criar. A matriz mostra *quais* trocas acontecem; a média esconderia exatamente isso.

#### Métricas reportadas

- Acurácia por campo e agregada
- Matriz de confusão de `intent`
- `parse_invalid_ratio` — saídas que quebraram o schema
- Acurácia por categoria — aponta **onde** melhorar, e quase sempre aponta para datas relativas
- Latência p50/p95 e tokens por caso (qualidade a que custo)

---

## Parte II — Resolução de referência

### 4. Por que tem conjunto próprio

A resolução não envolve LLM, mas seus pesos e limiares (§7.4 do PRD) são **arbitrários até serem calibrados**. Eles precisam de um instrumento tão sistemático quanto o do parser — com uma diferença: aqui o custo do erro é maior, e o conjunto de teste precisa de estado, não só de uma frase.

### 5. Cenários

Cada cenário é um estado de agenda mais uma referência, com o alvo esperado. Em `evals/scenarios/*.yaml`:

```yaml
- id: res-007
  now: "2026-09-19T10:00:00-03:00"
  timezone: "America/Sao_Paulo"
  fixtures:
    - {id: a, title: "Reunião com Fulano",  starts_at: "2026-09-20T14:00", status: pending}
    - {id: b, title: "Reunião de equipe",   starts_at: "2026-09-22T09:00", status: pending}
    - {id: c, title: "Reunião com cliente", starts_at: "2026-09-23T15:30", status: pending}
  reference: {terms: ["reunião"], time_hint: null}
  expect:
    outcome: disambiguated        # três candidatos plausíveis: NÃO agir
    candidates_include: [a, b, c]

- id: res-008
  # mesmos fixtures
  reference: {terms: ["reunião"], time_hint: {kind: day, date: "2026-09-20"}}
  expect:
    outcome: acted
    chosen: a
```

#### Categorias obrigatórias

| Categoria | O que testa |
|---|---|
| Alvo único óbvio | age sem perguntar |
| Vários candidatos similares | **pergunta** em vez de chutar |
| Dica temporal desempata | a margem aparece quando deveria |
| Paráfrase | "a luz" → "Conta de energia" — limitação conhecida do `pg_trgm` |
| Item inexistente | `not_found`, sem agir |
| Status incompatível | não conclui o que já está concluído |
| Item antigo fora da janela | não é alcançado, e isso é esperado |
| **Armadilha destrutiva** | referência vaga com vários alvos → nunca pode agir |

### 6. Métricas e pesos

| Métrica | O que responde |
|---|---|
| **Precisão em `acted`** | Quando agimos, foi no item certo? |
| `disambiguation_rate` | Com que frequência perguntamos? |
| `not_found_rate` | Com que frequência não achamos o que existia? |
| Distribuição de `top_score` e margem | Os limiares estão no lugar certo? |

**Precisão em ações destrutivas é ponderada separadamente.** Agir no item errado em `cancel` ou `update` conta com peso maior que qualquer outro erro do conjunto — é o risco crítico do §15 do PRD, e a média agregada o diluiria.

O trade-off a calibrar é explícito: limiares permissivos aumentam erro em `acted`; rígidos aumentam `disambiguation_rate` e trazem de volta o atrito que o produto existe para eliminar. O conjunto mede os dois lados ao mesmo tempo, porque otimizar um sem ver o outro é como se fabrica um resultado bonito e um produto ruim.

---

## Parte III — Operação

### 7. Execução

```bash
make eval                   # extração + resolução
make eval-parse             # só extração
make eval-resolve           # só resolução (não chama LLM, roda rápido)
make eval CATEGORY=rel      # uma categoria, durante iteração de prompt
make eval MODEL=...         # compara modelos no mesmo conjunto
```

Implementado como `go test -tags eval ./evals/...`, fora do `go test ./...` padrão: a parte de extração chama serviço externo, custa dinheiro e é lenta. Nunca no caminho do teste unitário.

`eval-resolve` é a exceção — é determinístico, roda contra Postgres de teste em segundos, e **pode entrar no CI de todo PR**.

Resultados vão para `evals/results/<timestamp>-<model>-<prompt_version>.json`, para que a comparação entre execuções seja histórica, não anedótica.

### 8. Gates de regressão

**Extração** — mudança em `prompt_version` ou de modelo exige rodar o eval e anexar o resultado ao PR.

Bloqueia se: queda de acurácia agregada > 2 p.p.; **ou** qualquer queda na categoria de datas relativas; **ou** qualquer novo erro na matriz de confusão que produza `cancel`, `complete` ou `update` onde se esperava `create` — a troca que gera ação destrutiva não pedida.

**Resolução** — roda em todo PR que toque `core/resolve` ou os limiares.

Bloqueia se: qualquer regressão na precisão de ações destrutivas; **ou** queda > 3 p.p. na precisão geral de `acted`; **ou** aumento > 5 p.p. em `disambiguation_rate` sem ganho correspondente em precisão.

### 9. Crescimento dos conjuntos

O golden set inicial é escrito à mão. Depois, cresce com dado real de produção, anonimizado, a partir de três fontes que a instrumentação já produz:

| Fonte | O que ela indica | Entra em |
|---|---|---|
| `undo` do usuário | O sistema agiu errado | Ambos os conjuntos |
| Desambiguação onde o usuário escolheu o 2º ou 3º candidato | A pontuação ordenou mal | Resolução |
| Desambiguação onde escolheu "Nenhuma dessas" | O alvo não estava entre os candidatos | Resolução |
| `not_found` seguido de `/pendentes` | O item existia e não foi alcançado | Resolução |
| Correção após pergunta de campo ausente | A extração errou | Extração |

Isso cria um ciclo: o produto em uso melhora o instrumento que mede o produto.

É também a justificativa de engenharia para o [ADR 0006](./adr/0006-confirmacao-seletiva-e-desfazer.md). O ADR 0004 valorizava a confirmação obrigatória como fonte de rótulo; a tabela acima mostra que os sinais que restaram são mais esparsos, porém mais honestos — ninguém desfaz uma ação no automático, enquanto todo mundo aperta "Confirmar" no automático.
