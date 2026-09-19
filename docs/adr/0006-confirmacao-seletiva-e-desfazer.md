# ADR 0006 — Confirmação seletiva e ações reversíveis

- **Status:** Aceito
- **Data:** 2026-09-19
- **Substitui:** [ADR 0004](./0004-confirmacao-sempre-no-v1.md)

## Contexto

O [ADR 0004](./0004-confirmacao-sempre-no-v1.md) estabeleceu que todo item passaria por confirmação explícita na v1. O argumento era defensável: a acurácia do parser era desconhecida e o modo de falha é assimétrico — um item criado com data errada em silêncio é pior que nenhum item.

O problema é o preço. A confirmação obrigatória cobra **de todos os usuários, em todas as ações**, um custo que só existe por causa dos casos em que o parser erra. Se a acurácia for de 90%, 90% dos toques em "Confirmar" são puro atrito — e atrito é exatamente o que o produto existe para eliminar (§1.1 do PRD). É o usuário pagando pela incerteza do sistema.

Ao mesmo tempo, o MVP passou a incluir mutação por linguagem natural (`complete`, `cancel`, `update`), o que aumenta a frequência de ações por usuário e multiplicaria o custo da confirmação obrigatória.

## Decisão

**O Noto age por padrão e oferece reversão. Confirmação prévia só quando é impossível agir.**

### 1. Executar direto, com desfazer

Interpretação completa e referência não ambígua → o sistema executa e responde com o **resultado**, não com uma proposta, acompanhado de um botão **Desfazer**.

### 2. Perguntar apenas quando a ambiguidade impede agir

Restam dois casos, e ambos são impossibilidade, não cautela:

- **campo obrigatório ausente** → pergunta só o campo faltante;
- **referência ambígua** (vários candidatos sem margem) → apresenta os candidatos.

### 3. Reversibilidade como pré-requisito, não como recurso

Toda mutação grava `item_revisions` com o estado anterior, na mesma transação. `cancel` é mudança de status, nunca `DELETE`. Desfazer aplica o snapshot e grava uma nova revisão `reverted` — a reversão é auditada, não é um apagamento.

Desfazer sobre um item que mudou depois daquela revisão **não sobrescreve em silêncio**: informa e mostra o estado atual (§5.6 do PRD).

### 4. Gatilho de retorno, por intenção

Cada "executa" é governado por um flag de configuração por intenção. Limites de `undo_rate` definidos em §10 do PRD — `cancel` no mais rígido (5%), `create` e `complete` no mais folgado (10%). Estourou o limite, aquela intenção volta a confirmar previamente. Sem deploy, sem mudança de código.

## Justificativa

1. **Desfazer entrega a mesma proteção, com distribuição de custo melhor.** Confirmar cobra de todos, sempre, antes. Desfazer cobra só de quem errou, só quando errou. Para a mesma garantia — nenhum dado perdido sem recurso — a segunda é estritamente mais barata em atrito.

2. **O eco continua existindo.** O benefício pedagógico citado no ADR 0004 — o usuário ver a interpretação estruturada e calibrar como escreve — é preservado integralmente. Ele acontece na mensagem de resultado, em vez de numa pergunta. O que se removeu foi o toque obrigatório, não a transparência.

3. **A rotulagem continua existindo, e fica mais honesta.** O ADR 0004 valorizava a confirmação como fonte de rótulo humano. `undo_rate` é um sinal melhor: confirmação tem viés de aquiescência — o usuário aperta "Confirmar" no automático — enquanto desfazer só acontece quando o resultado incomodou de verdade. É sinal com menos ruído, ainda que mais esparso.

4. **A assimetria de falha foi resolvida na camada certa.** O ADR 0004 tratava a assimetria pedindo permissão. O ADR 0006 a trata tornando **estruturalmente impossível** perder dado: nada é deletado, tudo tem snapshot. A proteção saiu do fluxo de interação e entrou no modelo de dados, onde não depende da atenção do usuário.

## Alternativas consideradas

**Manter a confirmação obrigatória.** Rejeitado pelo argumento de custo acima, agravado pela entrada de mutação por NL no escopo.

**Confirmar apenas em ações destrutivas** (`cancel`, `update`). Tem apelo, mas `cancel` é soft delete com snapshot — é tão reversível quanto as outras. Confirmar aí seria cerimônia sobre uma operação que já é segura. O caso genuinamente perigoso não é a intenção `cancel` em si, e sim **acertar o alvo errado**, e isso é resolvido pela margem mínima do resolver ([ADR 0007](./0007-resolucao-de-referencia.md)), não por um "tem certeza?".

**Confirmar com base na confiança auto-reportada pelo modelo.** Rejeitado, como no [ADR 0002](./0002-ollama-como-parser.md): LLMs são mal calibrados ao estimar a própria certeza, e o erro se concentraria nos casos confiantemente errados.

**Janela de desfazer curta, com auto-commit.** Rejeitado: acrescenta pressão de tempo e um estado intermediário para manter, sem benefício. O botão vale enquanto a revisão for a mais recente daquele item, o que é um critério de correção — não um cronômetro.

## Consequências

**Aceitas como positivas**

- O caminho comum vira uma mensagem e uma resposta. É a tese do produto executada em vez de declarada.
- `item_revisions` dá, de brinde, histórico por item e auditoria.
- `undo_rate` é um sinal de qualidade com menos viés que taxa de confirmação.
- A política é ajustável por intenção, em tempo de execução.

**Aceitas como custo**

- **Mais superfície para construir no M3.** Snapshot, reversão e tratamento de desfazer obsoleto precisam existir *antes* da primeira mutação automática. Executar sem confirmar antes de a reversão existir seria entregar o risco sem a rede — por isso `item_revisions` e o Desfazer são critério de pronto do M3, não item posterior.
- **O usuário pode não notar um erro.** Este é o custo real e honesto da decisão: quem não lê a mensagem de resultado não desfaz. Mitigado pelo fato de o erro ser sempre recuperável depois (nada é destruído) e por `undo_rate` ser acompanhado por intenção — mas não é eliminado.
- **O sinal é mais esparso.** Menos eventos de rotulagem que a confirmação obrigatória geraria. Em troca, cada evento vale mais. O golden set passa a crescer também a partir de casos de `undo` e de desambiguação, não só de correções.
