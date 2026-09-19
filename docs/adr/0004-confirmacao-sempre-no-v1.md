# ADR 0004 — Confirmação interativa em todo item na v1

- **Status:** ⚠️ **Substituído por [ADR 0006](./0006-confirmacao-seletiva-e-desfazer.md)** em 2026-09-19
- **Data:** 2026-09-19

> **Nota de supersessão.** Esta decisão foi revertida antes de qualquer implementação. O conteúdo abaixo permanece intacto porque o raciocínio continua válido — o que mudou foi a avaliação do trade-off: a proteção que a confirmação prévia oferecia passou a ser entregue por **execução reversível com desfazer**, que cobra o custo apenas de quem errou em vez de cobrar de todo usuário, em toda ação. O ADR 0006 registra o porquê e o que foi necessário construir para que a reversão fosse real. Conserva-se este documento porque uma decisão revertida rápido, com motivo registrado, é informação — e porque o critério de revisão definido aqui (§ *Critério de revisão*) sobrevive: ele é hoje o gatilho que **traz de volta** a confirmação prévia, por intenção, caso o `undo_rate` suba.

## Contexto

A tese do produto é **captura sem atrito** (§1.1 do PRD). Toda interação adicional é, em princípio, contrária a essa tese.

Ao mesmo tempo, a interpretação vem de um modelo de linguagem cuja acurácia real em PT-BR — especialmente em datas relativas — é desconhecida no início do projeto. E o modo de falha é assimétrico: um item criado com a data errada **em silêncio** é pior que nenhum item, porque o usuário confia e não confere.

## Decisão

**Na v1, todo item passa por confirmação explícita antes de ser criado.**

O bot ecoa a interpretação e oferece **Confirmar / Ajustar / Descartar**. Quando falta um campo, pergunta **apenas o campo faltante** — não repete a pergunta inteira.

A auto-confirmação para casos de alta certeza é implementada desde já, porém **desligada por flag de configuração**, pronta para ser ativada quando os dados justificarem.

## Justificativa

1. **O modo de falha é assimétrico.** Um toque num botão custa cerca de um segundo. Um compromisso perdido por data mal interpretada custa a confiança no produto inteiro — e confiança perdida não se recupera com uma correção de prompt. Princípio 3 do PRD.

2. **Confirmação é rotulagem gratuita.** Cada **Confirmar** é um rótulo positivo; cada **Ajustar** aponta exatamente onde o parser errou. Isso alimenta o crescimento do golden set com dado real de produção (§6 da [eval-strategy](../eval-strategy.md)). Sem confirmação, não haveria sinal nenhum sobre a qualidade em produção — só silêncio, que é indistinguível de sucesso.

3. **O eco ensina o usuário.** Ver a interpretação estruturada mostra o que o sistema entende bem, e o usuário calibra naturalmente a forma como escreve.

4. **É reversível na direção certa.** Remover uma confirmação depois é uma melhoria bem recebida. Adicionar confirmação depois de um incidente de compromisso perdido é uma admissão de falha.

## Alternativas consideradas

**Criação silenciosa, com edição posterior.** Menor atrito no papel. Rejeitado: sem `correction_rate` observável, não haveria como distinguir "o parser está ótimo" de "o usuário desistiu de corrigir". Voar sem instrumento na fase em que a qualidade é mais incerta.

**Confirmar apenas quando o modelo reportar baixa confiança.** Rejeitado — ver ADR 0002: a confiança auto-reportada por LLM é mal calibrada, e erraria justamente nos casos confiantemente errados, que são os perigosos.

**Confirmar apenas quando faltar campo obrigatório.** É a evolução planejada, não o ponto de partida. Falta o dado que justifica o limiar. O flag já existe para quando esse dado existir.

## Consequências

**Aceitas como positivas**

- Nenhum item entra na agenda com dado errado sem o usuário ver.
- `correction_rate` vira mensurável desde o M3, e com ela a qualidade real do parser.
- O eval set cresce com casos reais em vez de frases inventadas.

**Aceitas como custo**

- Dois toques em vez de zero. É atrito, e atrito é contrário à tese central — este ADR é uma concessão consciente, com prazo e critério de saída.
- **Risco declarado** (§15 do PRD): se a confirmação se mostrar irritante o suficiente para matar a retenção, a decisão precisa ser revista rápido. Por isso o flag de auto-confirmação é construído junto, e não depois.

## Critério de revisão

Após o M3, com volume real: se `correction_rate` < 5% de forma consistente para itens com data e hora completas, ativar auto-confirmação para essa classe, mantendo confirmação para os casos ambíguos.
