# message-ingestion

## ADDED Requirements

### Requirement: `IngestMessage` persiste a mensagem e responde com o eco

O caso de uso `IngestMessage` (`internal/app`) SHALL receber uma `message.Incoming` (id do update, id do usuário no Telegram, id do chat, texto), persistir o usuário e a mensagem numa **única transação** e, somente se a mensagem for nova, pedir ao `ports.Notifier` que envie ao mesmo chat um texto **idêntico** ao recebido. O usuário SHALL ser criado na primeira mensagem e reaproveitado nas seguintes, identificado por `telegram_user_id`. O envio SHALL ocorrer **depois** do commit da transação.

#### Scenario: Mensagem nova é persistida e ecoada

- **Given** um usuário nunca visto e uma `Incoming` com `update_id` 100, `chat_id` 7 e texto `oi`
- **When** `IngestMessage` processa a mensagem
- **Then** existe uma linha em `users` e uma em `messages` com `telegram_update_id` 100, `chat_id` 7 e `raw_text` `oi`, e o `Notifier` recebeu exatamente uma chamada com `chat_id` 7 e texto `oi`

#### Scenario: Usuário conhecido é reaproveitado

- **Given** um usuário já registrado e uma segunda mensagem sua com `update_id` diferente
- **When** `IngestMessage` processa a segunda mensagem
- **Then** continua havendo uma única linha em `users`, ambas as mensagens apontam para o mesmo `user_id`, e há duas respostas enviadas no total

#### Scenario: A resposta só sai depois do commit

- **Given** um `Notifier` que, ao ser chamado, consulta o banco por uma conexão independente
- **When** `IngestMessage` processa uma mensagem nova
- **Then** no instante da chamada ao `Notifier` a linha de `messages` já é visível para a conexão independente

#### Scenario: O texto é preservado byte a byte

- **Given** um texto com acentos, emoji, quebras de linha e 4096 caracteres
- **When** `IngestMessage` processa a mensagem
- **Then** `raw_text` no banco e o texto entregue ao `Notifier` são idênticos ao recebido

### Requirement: Idempotência de entrada por `telegram_update_id`

`IngestMessage` SHALL ser idempotente em `telegram_update_id` (PRD §8.4, ADR 0008 §2): processar de novo um update já registrado SHALL NOT criar linha em `messages`, SHALL NOT acionar o `Notifier` e SHALL terminar **sem erro**, sinalizando ao chamador que o update era duplicado. A garantia SHALL vir da restrição `UNIQUE` do banco, não de estado em memória, de modo que valha também entre reinícios e entre processos.

#### Scenario: Reentrega sequencial não duplica nem responde de novo

- **Given** um update já processado, com sua linha em `messages` e sua resposta enviada
- **When** o mesmo update (mesmo `update_id`) é entregue novamente
- **Then** `messages` continua com uma única linha para esse `update_id`, o `Notifier` não é chamado uma segunda vez, e a chamada devolve o resultado "duplicado" sem erro

#### Scenario: Reentrega concorrente resulta em uma linha e uma resposta

- **Given** o mesmo update entregue por N chamadas simultâneas
- **When** todas terminam
- **Then** existe exatamente uma linha em `messages` para esse `update_id`, o `Notifier` foi chamado exatamente uma vez, e exatamente uma das chamadas devolveu o resultado "nova"

#### Scenario: Reentrega depois de falha no envio não reenvia

- **Given** um update cuja persistência concluiu mas cujo envio falhou
- **When** o mesmo update é entregue novamente
- **Then** o resultado é "duplicado" e o `Notifier` não é chamado (o eco do M1 é *at-most-once*; ver "Resposta não usa outbox neste marco")

### Requirement: Falhas não deixam estado inconsistente nem resposta indevida

Uma `message.Incoming` inválida (id de update, id de usuário ou id de chat zerado; texto vazio) SHALL ser recusada com erro **antes** de qualquer escrita ou envio. Se a persistência falhar, `IngestMessage` SHALL devolver erro e SHALL NOT chamar o `Notifier`. Se o envio falhar depois de a mensagem ter sido persistida, a mensagem SHALL permanecer persistida, `IngestMessage` SHALL devolver o erro do envio, e SHALL NOT tentar reenviar.

#### Scenario: Entrada inválida é recusada sem efeito colateral

- **Given** uma `Incoming` com texto vazio
- **When** `IngestMessage` a processa
- **Then** devolve erro, `messages` e `users` não ganham linha e o `Notifier` não é chamado

#### Scenario: Falha do banco não gera resposta

- **Given** um repositório cuja conexão está indisponível
- **When** `IngestMessage` processa uma mensagem válida
- **Then** devolve erro e o `Notifier` não é chamado

#### Scenario: Falha do envio preserva a mensagem

- **Given** um `Notifier` que devolve erro
- **When** `IngestMessage` processa uma mensagem nova
- **Then** a linha em `messages` existe, o erro do envio é devolvido ao chamador, e o `Notifier` foi chamado uma única vez

### Requirement: Efeitos externos passam por portas

`IngestMessage` SHALL depender apenas das portas `ports.MessageStore` e `ports.Notifier`, declaradas em `internal/core/ports`, e de `message.Incoming`, declarada em `internal/core/message`. `internal/core/...` e `internal/app/...` SHALL NOT importar Telegram, `pgx` nem `internal/adapters/...`. O texto de resposta SHALL ser produzido pelo caso de uso, não pelo adapter.

#### Scenario: O caso de uso roda com um coletor em memória

- **Given** um `Notifier` que apenas registra chamadas em memória
- **When** `IngestMessage` processa mensagens
- **Then** o teste consegue afirmar *o que* seria enviado sem nenhuma rede

### Requirement: Resposta não usa outbox neste marco

No M1 a resposta SHALL ser enviada diretamente pelo `Notifier`, após o commit, **sem** passar pela tabela `outbox` (que não existe neste marco). Isso é uma exceção temporária e deliberada ao ADR 0003, válida porque o eco não altera estado de domínio que precise permanecer consistente com o envio; a semântica resultante é *at-most-once* para o eco. A substituição pela outbox transacional é escopo do M4.

#### Scenario: Nenhuma tabela `outbox` existe

- **Given** o banco migrado no M1
- **When** o catálogo é consultado
- **Then** não existe a tabela `outbox`, e a resposta de uma mensagem nova é enviada pelo `Notifier` diretamente

### Requirement: Logs do caso de uso contêm apenas identificadores

Todo log emitido por `IngestMessage`, em qualquer caminho (nova, duplicada, entrada inválida, falha de banco, falha de envio), SHALL conter apenas identificadores (`update_id`, e quando conhecidos `message_id`, `user_id`, `chat_id`) e SHALL NOT conter o texto da mensagem, nem em campo, nem embutido em texto de erro (PRD §12).

#### Scenario: O texto nunca aparece em log

- **Given** uma mensagem cujo texto é um marcador único e improvável
- **When** `IngestMessage` a processa em cada um dos caminhos: nova, duplicada, falha de banco e falha de envio
- **Then** a saída de log capturada não contém o marcador em nenhum caminho

#### Scenario: Os logs permitem rastrear o update

- **Given** uma mensagem nova, e depois o mesmo update reentregue
- **When** `IngestMessage` as processa
- **Then** há log com o `update_id` para o caso "nova" e outro, distinguível, para o caso "duplicada"
