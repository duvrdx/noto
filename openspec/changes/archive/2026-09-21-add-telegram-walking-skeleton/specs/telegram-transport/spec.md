# telegram-transport

## ADDED Requirements

### Requirement: Transporte por long polling dentro do `serve`

Com `TELEGRAM_TRANSPORT=polling`, o processo `noto serve` SHALL consultar `getUpdates` continuamente usando `github.com/go-telegram/bot` e SHALL entregar cada update aceito ao caso de uso `IngestMessage` (ADR 0008 §1). O polling SHALL rodar no `serve`, não no `worker`, e SHALL coexistir com `GET /healthz`. Apenas o tipo de update `message` SHALL ser solicitado (`allowed_updates`). O `offset` de cada consulta SHALL ser o maior `update_id` já recebido mais um. O adapter SHALL NOT deduplicar updates: a reentrega chega ao caso de uso, e a idempotência é do domínio (ADR 0008 §2).

#### Scenario: Update de texto chega ao caso de uso com os campos mapeados

- **Given** uma API de Telegram falsa que responde a `getUpdates` com um update `message` de texto, de chat privado, com `update_id`, `chat.id`, `from.id` e `text` conhecidos
- **When** o poller roda
- **Then** o caso de uso é chamado uma vez com `UpdateID`, `ChatID`, `TelegramUserID` e `Text` iguais aos do update

#### Scenario: O offset avança

- **Given** um lote com os updates 10 e 11
- **When** o poller consulta `getUpdates` de novo
- **Then** a nova consulta carrega `offset` 12

#### Scenario: Apenas `message` é solicitado

- **Given** o poller em execução
- **When** ele consulta `getUpdates`
- **Then** o parâmetro `allowed_updates` da consulta é exatamente `["message"]`

#### Scenario: Reentrega chega ao caso de uso

- **Given** a API falsa reentregando o update 10, já entregue antes
- **When** o poller roda
- **Then** o caso de uso é chamado de novo com o update 10 (o adapter não deduplica)

### Requirement: Só texto de chat privado é processado

O adapter SHALL entregar ao caso de uso somente updates com `message` de **texto não vazio**, com remetente (`from`) presente, em chat do tipo `private`. Qualquer outro update (foto, sticker, voz, mensagem sem remetente, grupo, canal, tipo de update inesperado) SHALL ser descartado: sem chamada ao caso de uso, sem persistência, sem resposta, com um log contendo o `update_id` e o motivo, e o offset SHALL avançar normalmente. `/start` e qualquer outro comando SHALL ser tratados como texto comum neste marco.

#### Scenario: Mídia sem texto é ignorada

- **Given** um update de mensagem com foto, sem `text`
- **When** o poller o recebe
- **Then** o caso de uso não é chamado, há um log com o `update_id` e o motivo, e a consulta seguinte avança o offset

#### Scenario: Mensagem de grupo é ignorada

- **Given** um update de texto cujo `chat.type` é `group`
- **When** o poller o recebe
- **Then** o caso de uso não é chamado e há um log com o `update_id` e o motivo

#### Scenario: Update sem remetente é ignorado

- **Given** um update de texto sem `from`
- **When** o poller o recebe
- **Then** o caso de uso não é chamado

#### Scenario: `/start` é texto comum

- **Given** um update de texto privado com `/start`
- **When** o poller o recebe
- **Then** o caso de uso é chamado com o texto `/start`

### Requirement: O ACK independe do resultado do processamento

O avanço do offset (a confirmação ao Telegram) SHALL NOT depender de o processamento ter dado certo: um erro devolvido pelo caso de uso SHALL ser logado e SHALL NOT parar o poller, SHALL NOT reconsultar o mesmo update e SHALL NOT atrasar o update seguinte (PRD §5.1, §9). Para que essa independência não vire perda em massa num crash, a fila interna entre o polling e o processamento SHALL ser mínima, de modo que o poller não confirme um lote inteiro que ainda não conseguiu entregar (*backpressure*).

#### Scenario: Falha no processamento não trava nem repete

- **Given** um caso de uso que devolve erro para o update 10, e um lote seguinte com o update 11
- **When** o poller roda
- **Then** o update 11 é entregue, nenhuma consulta volta a pedir o update 10 pelo offset, e há um log de erro com o `update_id` 10

#### Scenario: Backpressure limita o que é confirmado sem ser processado

- **Given** um lote de cinco updates e um caso de uso bloqueado no primeiro
- **When** o poller tenta prosseguir
- **Then** nenhuma consulta com offset maior que o do quarto update é feita enquanto o caso de uso permanecer bloqueado

### Requirement: Processamento serial e em ordem

Os updates SHALL ser processados **um por vez, na ordem de `update_id`**. Nenhum handler SHALL ser executado em goroutine própria e nenhum par de chamadas ao caso de uso SHALL se sobrepor no tempo dentro do adapter.

#### Scenario: Ordem preservada sem sobreposição

- **Given** um lote de três updates e um caso de uso que registra início e fim de cada chamada
- **When** o poller roda
- **Then** as chamadas ocorrem na ordem dos `update_id` e o fim de cada uma antecede o início da seguinte

### Requirement: Webhook não é suportado neste marco

Com `TELEGRAM_TRANSPORT=webhook`, `noto serve` SHALL encerrar na inicialização com código diferente de zero e uma mensagem que cita `TELEGRAM_TRANSPORT` e informa que apenas `polling` é suportado por enquanto. Essa verificação SHALL ocorrer antes de abrir o banco ou fazer qualquer chamada de rede. O valor `webhook` SHALL continuar sendo aceito pela validação de configuração, e nenhuma rota de webhook SHALL ser registrada.

#### Scenario: Serve recusa o webhook sem tocar em nada externo

- **Given** `TELEGRAM_TRANSPORT=webhook`, `TELEGRAM_WEBHOOK_SECRET` preenchido e um `DATABASE_URL` que aponta para lugar nenhum
- **When** `noto serve` inicia
- **Then** o processo encerra com código diferente de zero, a mensagem cita `TELEGRAM_TRANSPORT` e o `polling`, e o erro **não** é de conexão com banco

### Requirement: O token é validado na inicialização

Ao iniciar, o `serve` SHALL chamar `getMe` para validar o token. Token rejeitado pelo Telegram (401) SHALL encerrar o processo com código diferente de zero e uma mensagem que diz que o token foi rejeitado, sem conter o valor do token. Falha de rede na validação SHALL encerrar o processo com erro, também sem conter o token.

#### Scenario: Token rejeitado derruba o serve na subida

- **Given** uma API de Telegram falsa que responde 401 a `getMe`
- **When** o cliente Telegram é construído
- **Then** a construção devolve erro dizendo que o token foi rejeitado e o texto do erro não contém o token

### Requirement: Envio de mensagem em texto puro

A implementação de `ports.Notifier` no adapter SHALL enviar via `sendMessage` ao `chat_id` informado, com o texto exatamente como recebido, **sem `parse_mode`** (texto puro; nenhum caractere do usuário é interpretado como formatação). Erros da API (`403` usuário bloqueou o bot, `429`, `400`, rede) SHALL ser devolvidos ao chamador, sem retentativa, e o texto do erro SHALL NOT conter o token.

#### Scenario: Texto sai intacto e sem formatação

- **Given** um texto com `*`, `_`, `<b>` e `[x](y)`
- **When** o `Notifier` o envia
- **Then** a API falsa recebe `chat_id` e `text` idênticos aos informados e nenhum `parse_mode`

#### Scenario: Usuário que bloqueou o bot vira erro, não retentativa

- **Given** a API falsa respondendo 403 a `sendMessage`
- **When** o `Notifier` envia
- **Then** devolve erro que identifica o bloqueio, a API recebeu uma única tentativa, e o erro não contém o token

### Requirement: Segredos e conteúdo nunca aparecem em log nem em erro

O adapter SHALL substituir **todos** os manipuladores padrão de `go-telegram/bot` que emitem conteúdo: o manipulador de update padrão (que loga o update inteiro), o de erros (que loga, entre outros, o corpo bruto de um update que não decodifica) e o de debug. O modo de debug da biblioteca SHALL NOT ser habilitado. O adapter SHALL NOT usar o pacote global `log`, e nada SHALL ser escrito por ele. Nenhum log, erro devolvido ou saída de `stderr` produzido pelo adapter SHALL conter (a) o valor de `TELEGRAM_BOT_TOKEN`, nem (b) o texto de qualquer mensagem (PRD §12). Erros da biblioteca SHALL ser registrados por **classificação** (contexto cancelado, rede, conflito, limite de taxa, não autorizado, outro) mais o `update_id` quando conhecido — nunca por `err.Error()` bruto — e erros devolvidos ao chamador SHALL ter o token redigido como defesa adicional.

#### Scenario: Falha de rede não vaza o token

- **Given** um cliente configurado com um token conhecido apontando para um endereço em que ninguém escuta
- **When** a construção do cliente, um envio e uma rodada de polling falham
- **Then** nenhum erro devolvido e nenhuma linha de log contém o token

#### Scenario: Update malformado com texto do usuário não vaza o texto

- **Given** a API falsa entregando um update cujo JSON não decodifica e que contém um marcador de texto único
- **When** o poller o recebe e a biblioteca reporta o erro de decodificação
- **Then** a saída de log capturada não contém o marcador, mas registra que houve um erro de decodificação

#### Scenario: Operação normal não loga o texto

- **Given** updates de texto válidos com um marcador único, processados até o fim
- **When** a saída de log e a saída do pacote global `log` são capturadas
- **Then** nenhuma contém o marcador, e a saída do pacote global `log` está vazia

#### Scenario: Conflito de consumidores é diagnosticável

- **Given** a API falsa respondendo 409 a `getUpdates`
- **When** o poller recebe o erro
- **Then** há um log de classificação "conflito" que menciona webhook ativo ou outra instância consultando o mesmo bot, e o poller continua tentando com espera crescente, sem encerrar o processo

### Requirement: Encerramento gracioso do polling

Ao cancelamento do contexto, o poller SHALL parar de consultar `getUpdates`, SHALL concluir o update que já estiver em processamento (com prazo de 8 segundos por update, sob contexto próprio que não é cancelado pelo encerramento) e SHALL devolver `nil`. O `serve` SHALL encerrar o poller e o servidor HTTP juntos e só então fechar o pool de conexões.

#### Scenario: Update em andamento termina antes de o processo sair

- **Given** um caso de uso bloqueado no meio de um update
- **When** o contexto é cancelado
- **Then** o poller não retorna enquanto o caso de uso não termina, o contexto recebido pelo caso de uso não está cancelado, e ao terminar o poller devolve `nil`

#### Scenario: SIGTERM encerra o serve com código zero

- **Given** `noto serve` em execução com o polling ativo
- **When** o processo recebe `SIGTERM`
- **Then** poller e servidor HTTP param, o pool é fechado e o processo encerra com código zero
