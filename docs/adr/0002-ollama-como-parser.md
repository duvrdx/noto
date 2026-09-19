# ADR 0002 — Ollama como parser, cloud primeiro e local depois

- **Status:** Aceito
- **Data:** 2026-09-19

## Contexto

O núcleo do produto é converter fala livre em PT-BR em um objeto estruturado. Isso exige um modelo de linguagem com **saída estruturada confiável**, boa competência em português e resolução correta de expressões temporais relativas.

Há ainda uma restrição de privacidade: o texto processado é agenda pessoal — nomes, compromissos médicos, assuntos de trabalho. Mandar isso para um terceiro é uma decisão que precisa ter volta.

## Decisão

**Ollama como provedor de inferência, começando pelo Ollama Cloud, com portabilidade para instância local como requisito de arquitetura.**

A extração usa `POST /api/chat` com o campo `format` contendo um JSON Schema — structured output nativo. Verificado: o contrato é **idêntico** em `http://localhost:11434` e `https://ollama.com`; o cloud exige apenas o header `Authorization: Bearer $OLLAMA_API_KEY`.

Consequência direta: migrar para inferência local é trocar `OLLAMA_HOST` e remover a chave. Não é reescrita de adapter.

O modelo é tratado como **componente não confiável**:

- o prompt recebe `now` no fuso do usuário e o nome IANA do fuso — não se assume que o modelo saiba que dia é hoje;
- toda saída é revalidada em Go (data no passado, a mais de dois anos, fuso inconsistente → rejeitada);
- um retry com o erro de validação realimentado; duas falhas → fallback que preserva a mensagem bruta;
- **o gate de confirmação é determinístico em Go**, com base em campos ausentes e ambiguidade — nunca em pontuação de confiança auto-reportada pelo modelo, que é notoriamente mal calibrada.

Atrás de `internal/core/ports.Parser`. Nenhum tipo do pacote `ollama` atravessa essa fronteira.

`prompt_version`, `schema_version`, modelo, latência, tokens e saída bruta são gravados em `parse_runs` a cada chamada.

## Alternativas consideradas

**API proprietária de LLM (Anthropic, OpenAI).** Provavelmente entregaria acurácia superior em PT-BR hoje. Rejeitado porque não oferece caminho para inferência local: a decisão de privacidade se tornaria irreversível, e o custo por mensagem seria estrutural. A interface `Parser` mantém essa porta aberta caso a qualidade do Ollama se mostre insuficiente no eval.

**SLM local desde o dia 1** (Ollama na máquina). Rejeitado como ponto de partida: obriga a resolver qualidade de extração e operação de inferência simultaneamente, e amarra a iteração de prompt ao hardware local. Cloud primeiro permite iterar rápido; a migração para local vira uma decisão medida, não uma restrição inicial.

**Parsing por regra** (regex + biblioteca de datas). Rejeitado como solução principal — cobre "amanhã às 14h" e quebra em "fim da tarde de sexta", que é exatamente a fala natural que o produto promete entender. Permanece disponível como possível pré-filtro de otimização de custo, não como estratégia.

**Confiar na confiança auto-reportada do modelo.** Rejeitado: LLMs são mal calibrados ao estimar a própria certeza, e usar esse número como gate de confirmação produziria falsos negativos silenciosos — a pior falha possível neste produto.

## Consequências

**Aceitas como positivas**

- Qualidade e velocidade de iteração de prompt no início, quando é o que mais importa.
- O caminho para local existe e é barato, o que mantém a decisão de privacidade reversível.
- `parse_runs` dá base empírica para escolher modelo em vez de escolher por impressão.

**Aceitas como custo**

- Texto pessoal trafega para terceiro. Precisa ser declarado ao usuário no `/start`, sem eufemismo (§12 do PRD).
- Custo por mensagem e dependência de disponibilidade externa. Mitigados por métrica de tokens com alarme de teto, e pelo fallback de §5.3 do PRD.
- Modelos do catálogo cloud mudam ao longo do tempo. É precisamente por isso que o eval existe ([eval-strategy](../eval-strategy.md)) — troca de modelo passa a ser verificável.
