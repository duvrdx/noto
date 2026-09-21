# Design — M1, walking skeleton

## 1. O que este documento decide (e o que não)

Só decide o que o M0 e o scaffolding deixaram em aberto por ser detalhe de implementação ou por depender do que o código real permite. Tudo que o PRD, a arquitetura ou um ADR já fixaram é citado, não redecidido: camadas (PRD §8.2), biblioteca de Telegram (§8.3), transportes (ADR 0008), idempotência por `telegram_update_id` (§8.4), pirâmide e dublês (`testing.md`), Go `1.26.0`.

Onde uma decisão **diverge de uma frase de um documento existente**, isso é dito em voz alta e vai para *Questões em aberto* no `tasks.md`: PRD §14 diz "webhook recebe" (o M1 recebe por polling); ADR 0003 diz "toda mensagem ao usuário passa pela outbox" (o eco do M1 não passa); o `design.md` do scaffolding §10 diz que "é o M1 que começa a exportar" telemetria (o M1 não exporta).

Fatos sobre a biblioteca abaixo foram lidos **no código-fonte de `github.com/go-telegram/bot v1.27.0`**, não da documentação; estão marcados como *(fonte)*.

## 2. Decisões

### 2.1 Polling no M1, webhook fica para depois

**Decisão:** o M1 implementa **somente** long polling. `TELEGRAM_TRANSPORT=webhook` continua válido na configuração, mas o `serve` recusa iniciar com erro explícito (spec `telegram-transport`).

Razões:

- **O critério de pronto não precisa dele.** "Mensagem aparece em `messages` e recebe resposta" se cumpre por polling, e o ADR 0008 escolhe polling para desenvolvimento justamente para o M1 não depender de túnel.
- **O webhook traz superfície que nenhum documento definiu.** Exige HTTPS público, o **caminho do endpoint não está em nenhum documento** (PRD §8.8 e o ADR 0008 falam em "webhook + `secret_token`" sem rota), o ciclo de vida do `setWebhook`/`deleteWebhook`, e a validação do cabeçalho `X-Telegram-Bot-Api-Secret-Token`. Cada um é decisão de produção; decidi-los sem poder testá-los contra o Telegram real (não há HTTPS público na máquina de desenvolvimento) seria escrever código sem verificação possível.
- **O custo de adiar é baixo, e a costura fica pronta.** O mapeamento `models.Update → message.Incoming` e a entrega ao caso de uso são o mesmo código para os dois transportes (ADR 0008 §1); o webhook será um handler HTTP de dezenas de linhas sobre `bot.WebhookHandler()` *(fonte: existe e valida o `secret_token`)*. Os testes de contrato do adapter (§2.10) são escritos de modo que o segundo transporte seja acrescentado a uma tabela, não reescrito.

**Alternativa rejeitada — incluir o webhook agora.** Fecharia a lacuna entre PRD §14 ("webhook recebe") e a implementação, mas exigiria: definir a rota, escolher como e quando registrar o webhook, e uma verificação em produção que o usuário só pode fazer numa VPS com TLS — que também não existe ainda. Ficaria código não exercitado no `main`, o pior lugar para ele.

**Se o usuário preferir incluir o webhook:** rota proposta `POST /telegram/webhook` no mesmo `ServeMux`; `secret_token` de `TELEGRAM_WEBHOOK_SECRET` (já obrigatório sob `webhook`); `setWebhook` **não** é chamado pela aplicação (registro manual, uma vez, documentado no README), para o processo não ter efeito colateral na conta do bot ao subir; ACK `200` devolvido depois do enfileiramento e antes do processamento. Isso adicionaria ~3 tasks e uma seção de spec; a decisão é da Questão em aberto nº 1.

**Consequência do polling assumida:** dois processos consultando o mesmo bot recebem `409 Conflict`; e um webhook ativo também o causa. Por isso o `serve` é o único poller (o Compose tem uma instância) e o erro 409 é *classificado* no log com uma mensagem acionável (spec). A aplicação **não** chama `deleteWebhook` na subida: com um único token compartilhado entre dev e futura produção, um `deleteWebhook` automático em dev derrubaria a produção.

### 2.2 Sem outbox no M1 — exceção temporária ao ADR 0003

**Decisão:** a resposta é enviada **diretamente** pelo `Notifier`, depois do commit, sem a tabela `outbox`.

O que o ADR 0003 exige, e por quê: a outbox existe para que "mensagem de saída" e "mudança de estado do domínio" sejam **atômicas** — o exemplo que ele rejeita é "lembrete marcado como enviado, mensagem nunca enviada". O M1 não tem esse acoplamento:

- **Não há estado de domínio que dependa do envio.** A linha de `messages` é a *entrada*, gravada antes; nada é marcado "respondido". Se o envio falha, o estado do banco continua verdadeiro.
- **A perda do eco é benigna.** Uma resposta que não chega no M1 é o eco de um skeleton; não é um lembrete que deixou de disparar.
- **A outbox pressupõe o despachante**, e o despachante é o M4 (PRD §14: "Scheduler, outbox, entrega de lembrete"). Construir a tabela, a query de *claim* com `SKIP LOCKED`, o backoff, a `dedup_key` e o laço no `worker` seria adiantar o M4 inteiro para dentro do "esqueleto". Pior: a **forma da porta** dependeria de uma decisão de transação que só o M3 (mutação + revisão + saída na mesma transação, PRD §8.5) vai forçar — projetá-la agora é um chute sobre a dependência, o mesmo tipo de chute que o scaffolding §10 recusou ao não escrever portas sem consumidor.

**O que se perde e é aceito:** a garantia de resposta. A semântica do eco é *at-most-once*: se o processo cai entre o commit e o `sendMessage`, o update reentregue é reconhecido como duplicado (o `UNIQUE` fez seu trabalho) e **não** é respondido de novo. O spec documenta isso como cenário, para ninguém descobrir em produção que o "ecoa" tem esse furo. É exatamente o furo que a outbox fecha, no M4.

**Custo de desfazer no M4:** `Notifier.Notify(ctx, chatID, text)` continuará sendo a porta vista pelos casos de uso de *saída*; o M4 troca a implementação por uma que grava na `outbox` (e, quando o M3 introduzir a transação de mutação, a porta ganha o parâmetro de transação — mudança de assinatura conhecida e localizada). Isso é registrado aqui e como Questão em aberto nº 2, para o usuário confirmar que aceita a exceção; **se ele preferir a outbox já no M1**, o M1 passa a incluir o M4 parcial (tabela, dispatcher no `worker`, `dedup_key = "echo:" + update_id`) e o plano cresce em cerca de seis tasks (tabela, query de *claim*, despachante no `worker`, backoff, testes de concorrência).

**Alternativa rejeitada — enviar antes de persistir.** Garantiria resposta, mas responderia a mensagens que nunca foram registradas e duplicaria a resposta em qualquer reentrega. Pior que a exceção escolhida.

### 2.3 O que `go-telegram/bot` impõe: ACK, ordem e o que ela loga *(fonte, v1.27.0)*

Pontos lidos no código que moldam o adapter:

1. **O offset avança antes do handler.** `getUpdates` grava `lastUpdateID` e só então empurra o update para um canal; a confirmação ao Telegram é o `offset` da consulta seguinte. Ou seja, o ACK é **estruturalmente independente** do resultado do processamento (o que o PRD §5.1/§9 pede), mas isso tem um custo real: **se a persistência falhar, o update se perde** — o Telegram já o considera entregue. Idem se o processo cair com updates no canal. O webhook da biblioteca (`WebhookHandler`) tem a mesma propriedade (enfileira e devolve `200`).
2. **O canal interno tem capacidade 1024 por padrão** — até 1024 updates "confirmados e ainda não processados" na memória. **Decisão:** `WithUpdatesChannelCap(1)` + `WithWorkers(1)` + `WithNotAsyncHandlers()`. Com isso o poller **bloqueia** ao enfileirar quando o processamento está ocupado, o offset avança só até onde o poller conseguiu entregar, e a exposição a crash fica em ~2–3 updates em vez de 1024. Processamento **serial e em ordem** é o que o M1 quer (dezenas de mensagens por dia; ordem por usuário importará no M3), e `NotAsync` faz `bot.Start` esperar o handler em andamento ao encerrar — dá o *graceful shutdown* de graça.
3. **Padrões que vazam.** O handler de update padrão faz `log.Printf("%+v", update)` (o **texto do usuário**); o handler de erros padrão loga via `log` global, e algumas mensagens de erro da biblioteca embutem o **corpo bruto** do update (`error decode update, %s`) e o **corpo da resposta** da API; o modo debug imprime payloads. **Decisão:** substituir os três handlers (`WithDefaultHandler`, `WithErrorsHandler`, `WithDebugHandler`), nunca usar `WithDebug`, e registrar erros da biblioteca por **classificação** (`errors.Is`/`errors.As` sobre `ErrorConflict`, `ErrorUnauthorized`, `*TooManyRequestsError`, `*url.Error`, `context.Canceled`, senão "outro") mais o `update_id` quando o erro o traz — nunca `err.Error()`. Não dá para higienizar por *regex* um texto livre do usuário; classificar é o único caminho que fecha por construção.
4. **O token está no caminho da URL** (`/bot<token>/<método>`). A biblioteca já troca o token por `***` no `*url.Error` de falha de rede, mas não cobre o erro de `http.NewRequest` nem o `getMe` do `New`. **Decisão:** todo erro devolvido pelo adapter passa por um redator que troca o valor do token por `[REDACTED]`, como defesa em profundidade, e há teste com um token sentinela contra falha de rede (§2.10). O `cli.run` escreve erros de subcomando em `stderr` (`main.go`), então a redação precisa acontecer **antes** de o erro sair do adapter.
5. **`bot.New` chama `getMe`** (timeout 5 s) e falha se o token é rejeitado. Aproveitado como **validação do token na subida**, que é o objetivo da modificação da spec `configuration`: token errado derruba o `serve` com mensagem clara, não a primeira mensagem do usuário. Consequência: `serve` **exige rede** para subir.

Um `bot.Bot` só, construído em duas fases porque há um ciclo de dependência (o caso de uso precisa do `Notifier`, o `Notifier` do bot, o bot do handler): `telegram.NewClient(cfg)` → `client.Notifier()` → `app.NewIngestMessage(store, notifier, log)` → `client.Run(ctx, handler)`. O `Client` recebe a URL base da API por `Config` (padrão `https://api.telegram.org`) para os testes apontarem ao fake; **não** há variável de ambiente nova (a spec `configuration` fixa o conjunto).

### 2.4 Camadas e portas mínimas

- `internal/core/message`: `Incoming{UpdateID, TelegramUserID, ChatID int64; Text string}` e `Validate()` (ids não zerados, texto não vazio). É a única entidade nova do domínio no M1; um pacote a mais em `core` dá ao teste de fronteira algo real para guardar e mantém o vocabulário do domínio fora do adapter. Ids de linha (`message_id`, `user_id`) são `string` (UUID em texto): `core` não importa `pgtype`.
- `internal/core/ports`: `MessageStore` com `SaveIncoming(ctx, message.Incoming) (SaveResult, error)` — `SaveResult{UserID, MessageID string; Created bool}` — e `Notifier` com `Notify(ctx, chatID int64, text string) error`. Só essas duas: nenhuma interface sem implementador e consumidor no M1 (`Parser`, `Resolver`, `Clock` ficam para quem as usa).
- **A transação vive dentro de `SaveIncoming`**, no adapter, e não numa unidade de trabalho exposta ao `app`. Justificativa: no M1 há uma única operação atômica de duas escritas; expor `UnitOfWork` agora seria projetar a abstração do M3 (mutação + revisão + saída) sem o caso que a forma. PRD §8.5 põe a orquestração transacional em `app`; o M3 é quem a introduz. Registrado como custo conhecido.
- **Sem `ports.Clock` no M1.** `received_at` e `created_at` usam `DEFAULT now()` do banco. O "Clock injetável" do PRD §8.6 existe para o domínio calcular datas; o M1 não calcula nenhuma. Introduzir um `Clock` que só alimenta uma coluna de auditoria seria porta sem consumidor de domínio. O teste de expurgo do M6 insere `received_at` explícito, não precisa do relógio.
- O texto da resposta é decisão do **caso de uso** (o eco), não do adapter: o adapter entrega bytes, o `app` decide o que dizer.

### 2.5 Esquema: `users` e `messages`, só o que o M1 usa

PRD §6.2 é o alvo. O M1 grava e lê o seguinte; o resto sai, coluna a coluna.

**`users`**

| Coluna | Decisão | Justificativa |
|---|---|---|
| `id uuid pk default gen_random_uuid()` | entra | `messages.user_id` referencia; `gen_random_uuid()` é nativo desde o Postgres 13 (a imagem é 16), sem `pgcrypto` e sem dependência de UUID no Go |
| `telegram_user_id bigint NOT NULL UNIQUE` | entra | é a chave do upsert; sem ela não há "primeira mensagem" vs "usuário conhecido" |
| `created_at timestamptz NOT NULL default now()` | entra | barato, e o único metadado de ciclo de vida que o `/esquecer` e a retenção vão querer |
| `timezone` | **fora** — ver §2.5.1 | |
| `locale` | **fora** | nada lê; tem default constante `'pt-BR'`, então acrescentá-la depois (`ADD COLUMN ... NOT NULL DEFAULT 'pt-BR'`) é aditivo e não exige backfill |

**`messages`**

| Coluna | Decisão | Justificativa |
|---|---|---|
| `id uuid pk default gen_random_uuid()` | entra | identidade da linha; logada como `message_id` |
| `user_id uuid NOT NULL` → `users(id)` | entra | PRD §6.2; `NOT NULL` porque toda mensagem do M1 tem dono |
| `telegram_update_id bigint NOT NULL UNIQUE` | entra | a idempotência. O PRD escreve só `UNIQUE`; aqui é também `NOT NULL` porque toda linha do M1 vem de um update, e um `NULL` seria um jeito de contornar o `UNIQUE` (Postgres trata `NULL`s como distintos) |
| `chat_id bigint NOT NULL` | entra | o eco vai para lá; PRD §6.2 |
| `raw_text text NOT NULL` | entra | é o conteúdo; `NOT NULL` porque o M1 só persiste updates de texto (§2.7). Quando mídia entrar (v2), a coluna passa a nullable numa migração deliberada |
| `received_at timestamptz NOT NULL default now()` | entra | retenção de 90 dias (PRD §12) e auditoria; é o momento de recebimento **pelo Noto**, não o `message.date` do Telegram |
| `telegram_message_id` | **fora** | só serviria a *reply-to* e edição; nada disso no M1 |
| índice em `messages(user_id)` ou `received_at` | **fora** | nenhuma query do M1 filtra por eles; índice sem consulta é custo de escrita sem retorno. O `/esquecer` e o expurgo (M6) criam o que precisarem |
| `ON DELETE CASCADE` na FK | **fora** | o PRD §12 quer `/esquecer` deletando *de propósito* na ordem; cascata é decisão daquele marco, e a ausência agora falha fechado (impede deletar usuário com mensagens por engano) |

Migração: `migrations/00002_create_users_and_messages.sql`, `Up` cria `users` depois `messages`; `Down` remove `messages` depois `users` e **não** toca em `pg_trgm` (essa é da `00001`).

O `sqlc` lê o schema de `migrations/` ignorando o bloco `-- +goose Down` *(scaffolding, verificado)*; o código gerado em `internal/adapters/postgres/db/` ganha `models.go` com `User` e `Message` e os métodos das duas queries. `pgtype.UUID` entra no código gerado (default do `sqlc` com `pgx/v5`); a conversão para `string` fica no adapter, não em override de tipo, para não introduzir `github.com/google/uuid`.

#### 2.5.1 `users.timezone` fica para o marco do onboarding

PRD §4.1 F1 e ADR 0005 §2 dizem que o fuso é **capturado no `/start` e confirmado explicitamente**, com default `America/Sao_Paulo`. Duas leituras possíveis para o M1: (a) criar `timezone NOT NULL` já, preenchida com `DEFAULT_TIMEZONE`; (b) deixar de fora.

**Decisão: (b).** Razões:

- **Nada no M1 lê o fuso.** Não há `timex`, item, lembrete nem "hoje". Uma coluna que só é escrita é dado sem consumidor.
- **Preencher com o default mistura "padrão" com "confirmado".** O ADR 0005 exige confirmação explícita; se o M1 grava `America/Sao_Paulo` para todo mundo, o onboarding depois não sabe distinguir quem confirmou de quem só recebeu o valor. Isso pediria uma segunda coluna (`timezone_confirmed_at`) ou um backfill ambíguo. Sem a coluna, "sem fuso" continua sendo um estado representável e verificável — exatamente o que o onboarding precisa perguntar.
- **O custo de adiar é conhecido e pequeno.** O marco do onboarding adiciona `timezone text` (nullable + confirmação, ou `NOT NULL` com backfill de `DEFAULT_TIMEZONE`) sobre `users` que hoje só têm linhas de desenvolvimento.
- `DEFAULT_TIMEZONE` continua sendo lida e validada pela configuração e **não é consumida** no M1. É deliberado.

*Alternativa rejeitada (a):* casa com a letra do PRD §6.2 (`NOT NULL`) mas pela razão errada — satisfaz o esquema sacrificando a semântica de confirmação.

Registrado como Questão em aberto nº 3: o usuário decide se prefere (a).

### 2.6 Idempotência: o banco diz o que é novo

`SaveIncoming` abre uma transação `pgx` e executa duas queries:

1. `UpsertUser`: `INSERT ... ON CONFLICT (telegram_user_id) DO UPDATE SET telegram_user_id = EXCLUDED.telegram_user_id RETURNING id` — o `UPDATE` sem efeito existe só para o `RETURNING` devolver o `id` também no conflito (com `DO NOTHING` o Postgres não devolve linha). Roda **mesmo para update duplicado**: é inócuo, e o usuário existe de qualquer forma.
2. `InsertMessage`: `INSERT ... ON CONFLICT (telegram_update_id) DO NOTHING RETURNING id` (`:one` no `sqlc`). **Sem linha devolvida** (`pgx.ErrNoRows`) ⇒ duplicado ⇒ `Created=false`. Com linha ⇒ `Created=true`.

Commit em ambos os casos. Quem decide se há eco é `Created`, vindo do banco: vale entre reinícios, entre processos e sob concorrência (a spec tem o cenário com N chamadas simultâneas, provado contra Postgres real). Sob concorrência, o segundo `INSERT` bloqueia até o primeiro commitar e então encontra o conflito — comportamento garantido do `ON CONFLICT`, não sorte de timing.

*Alternativa rejeitada:* `SELECT` antes do `INSERT` — abre a janela check-then-act que o `UNIQUE` existe para eliminar. *Alternativa rejeitada:* CTE única `WITH u AS (...) INSERT ... SELECT ... FROM u` — atômica sem transação explícita, mas dois statements curtos numa transação são mais legíveis e não dependem de o `sqlc` v1.31 tipar CTE com escrita (não verifiquei). Se a implementação preferir a CTE e o `sqlc` aceitar, é troca local ao adapter, coberta pelos mesmos testes.

`SaveIncoming` recebe uma interface mínima `Begin(ctx) (pgx.Tx, error)`, satisfeita por `*pgxpool.Pool` **e** por um `pgx.Tx` (cujo `Begin` cria *savepoint*). É isso que permite ao helper de teste (§2.10) entregar uma transação com rollback ao teste e o repositório ainda abrir "a sua" transação por dentro.

### 2.7 O eco e o que é ignorado

- **Eco literal, sem prefixo.** A resposta é o próprio texto. Duas razões práticas: não há redação de produto a decidir no M1 (o wording de verdade nasce quando houver o que dizer), e o eco literal **nunca excede** o limite de 4096 caracteres do `sendMessage`, o que um prefixo ("Recebi: ...") poderia estourar em mensagens longas e transformar o skeleton em fonte de `400`.
- Texto puro: sem `parse_mode`, para `*`, `_` ou `<` do usuário não virarem formatação nem erro `400`.
- Só `message` de texto, `from` presente, `chat.type == "private"` (persona: uso pessoal, PRD §2). O resto é descartado **com log** (`update_id` + motivo) e o offset avança. Grupo, canal, mídia e voz são escopo posterior (voz é v2, PRD §14).
- `/start` e demais comandos são texto comum: onboarding é F1, fora do M1.
- `allowed_updates=["message"]` reduz na origem: `edited_message`, `callback_query` etc. nem chegam. O filtro do adapter continua existindo porque `allowed_updates` é uma otimização de banda, não uma garantia (o Telegram pode mantê-lo de uma configuração anterior).

### 2.8 Privacidade: dois vazamentos, um mecanismo para cada

PRD §12 é categórico: logs **nunca** contêm texto de mensagem. O scaffolding já fechou o token na `Config` (`LogValue`); o M1 introduz três novos pontos de saída, todos tratados acima (§2.3):

| Ponto | Vetor | Fechamento |
|---|---|---|
| Caso de uso | log de "mensagem recebida" com o texto por conveniência | só identificadores; teste com marcador único em todos os caminhos |
| Biblioteca | handlers padrão, corpo bruto em erro, debug | substituição dos três handlers, classificação em vez de `err.Error()` |
| Adapter | token no caminho da URL em erro de rede / `getMe` | redação do valor do token em todo erro devolvido, mais teste com token sentinela |

O teste com marcador único captura **três saídas** — o `slog` injetado, o pacote global `log` (redirecionado no teste; deve permanecer vazio) e o `stderr` da CLI — porque é o que a biblioteca faria por padrão. Sem o teste do `log` global, a substituição dos handlers seria uma intenção; com ele, é uma propriedade verificada.

### 2.9 Configuração: token obrigatório para todos os subcomandos

**Decisão:** `TELEGRAM_BOT_TOKEN` passa a ser obrigatório em `config.Load`, portanto para `serve`, `worker` **e** `migrate`.

A regra do scaffolding era "obrigatória quando algum código falha sem ela". Estritamente, só o `serve` falha sem o token no M1; o `worker` (tick vazio) e o `migrate` não. Mesmo assim: (a) `cli.run` carrega e valida a configuração **uma vez, antes do despacho** — exigir só no `serve` obrigaria a validação a se dividir em duas etapas e perderia a propriedade "todas as variáveis problemáticas numa mensagem só"; (b) o `worker` precisará do token no M4 para despachar a outbox, então a exigência só seria adiada; (c) todos os processos leem o mesmo `.env`. **Consequência aceita e documentada na spec:** `noto migrate` também recusa rodar sem token, e um `.env` copiado cru de `.env.example` deixa de subir qualquer coisa. Se isso incomodar (por exemplo, migração numa pipeline de deploy sem o segredo), a saída é um `Load` por modo — Questão em aberto nº 5.

Não valido o *formato* do token (`\d+:[\w-]+`): o Telegram pode mudá-lo, e o `getMe` da subida é a validação autoritativa.

Efeito colateral em testes existentes que precisam ser ajustados: os casos de `config_test.go` que exigem "token vazio não é erro", e os testes de `cmd/noto` que definem só `DATABASE_URL` via `t.Setenv` (`TestDefaultCLIInvalidLogLevelExitsNonZero`, o de `migrate_test.go`) — passariam a falhar por `TELEGRAM_BOT_TOKEN`, e não pelo motivo que testam. O mesmo vale para `TestNoMigrationCreatesTables` em `migrations/embed_test.go`, que varre **todas** as migrações e reprovaria a `00002`: passa a valer só para a `00001`.

### 2.10 Testes e CI

Pirâmide do `testing.md` aplicada ao M1:

| Camada | O que | Onde |
|---|---|---|
| Unitário | `message.Incoming.Validate`; mapeamento `models.Update → Incoming` e motivos de descarte; classificador/redator de erros | `core/message`, `adapters/telegram` — sem setup |
| Integração | schema, restrições e `Down` da migração; `SaveIncoming` (novo, duplicado, mesmo usuário, concorrência); pool | `internal/platform/migrations`, `internal/adapters/postgres` — Postgres real |
| Caso de uso | `IngestMessage` com repositório **real** + `Notifier` coletor | `internal/app` — Postgres real |
| Contrato de canal | o adapter Telegram contra uma API falsa: entrega, offset, filtros, ACK, ordem, backpressure, privacidade, encerramento, `Notifier` | `adapters/telegram` |
| Ponta a ponta automático | fake Telegram + Postgres real + `serve` fiado: mensagem → linha + eco; reentrega → nada novo | `cmd/noto` |
| Ponta a ponta real | bot real, token real, Compose | task 10.1, `owner: human` |

**Infraestrutura nova em `internal/testutil/` (código de teste, não faz parte do binário):**

- `testdb`: contêiner `postgres:16` por execução de pacote de teste (`TestMain` + `sync.Once`), migrações aplicadas uma vez via `platform/migrations.Up`, cada teste numa transação com rollback (`testing.md` §3). Um segundo modo, `testdb.Pool(t)`, cria um **banco novo no mesmo contêiner** para quem precisa de visibilidade entre conexões (concorrência, "resposta só depois do commit", teste de `Down`) e o descarta ao fim. `-short` faz `t.Skip`; sem `-short` e sem Docker, **falha alto** (um pulo silencioso faria a suíte mentir).
- `tgfake`: servidor `httptest` que emula `getMe`, `getUpdates` e `sendMessage`. Exige o token no caminho (`/bot<token>/...`), grava os `offset` recebidos e as mensagens enviadas, aceita lotes roteirizados (incluindo reentrega, JSON malformado e respostas 401/403/409/429), e simula o *long poll* segurando a resposta por pouco tempo quando não há lote (para o laço da biblioteca não girar a toda velocidade).
- `notifiertest`: `ports.Notifier` coletor em memória (`testing.md` §2), com variante que devolve erro.

**Casos inegociáveis do `testing.md` §4 aplicáveis ao M1** e onde caem: *update reentregue não executa a ação duas vezes* (`message-ingestion`, três cenários, o concorrente incluído); *ACK independente do processamento* (`telegram-transport`, dois cenários); *texto nunca em log* e *token nunca em log/erro* (`telegram-transport` + `message-ingestion`). Os demais casos (reversibilidade, concorrência de lembrete, resolução, tempo) não têm código no M1.

**CI (`ci.yml`) — verificado, sem mudança.** O runner `ubuntu-latest` tem Docker e `go test -race ./...` já é passo do workflow; testcontainers com o *reaper* funciona sem configuração no runner hospedado. Não declaro `services: postgres` no workflow: duplicaria o que o testcontainers já faz e criaria dois Postgres a manter. Pontos a vigiar (Questões em aberto): primeira execução puxa `postgres:16` e a imagem do *reaper* do Docker Hub (limite de taxa anônimo é raro mas existe); `timeout-minutes: 15` folgado para isso. `govulncheck ./...` roda sem `-test`; testei um módulo-amostra com `-test` para cobrir as dependências de teste, e saiu limpo.

**`make test`** continua `go test -race ./...` e agora **exige Docker**. Quem quer o ciclo rápido usa `go test -short ./...`. Não crio alvo novo no Makefile: seria uma segunda forma de fazer a mesma coisa.

### 2.11 Dependências

| Módulo | Versão | `go` exigido | Uso | Observação |
|---|---|---|---|---|
| `github.com/go-telegram/bot` | `v1.27.0` (última tag) | `1.18` | produção, só `adapters/telegram` | zero dependências transitivas; proibida em `core` e `app` pelo teste de fronteira |
| `github.com/testcontainers/testcontainers-go` | `v0.44.0` | `1.25.0` (toolchain `1.25.9`) | teste | traz `docker`/`moby`, `otel`, `x/crypto` etc. transitivos |
| `github.com/testcontainers/testcontainers-go/modules/postgres` | `v0.44.0` | `1.25.0` | teste | traz `lib/pq`; declara `pgx v5.9.2`, mas a MVS mantém o `v5.11.0` do repositório |
| `golang.org/x/sync` | `v0.23.0` (**já** no `go.mod`, indireta) | `1.26.0` | produção, `errgroup` no `serve` | apenas passa de indireta a direta |
| `github.com/jackc/pgx/v5/pgxpool` | (do `pgx v5.11.0`) | — | produção | sem módulo novo: `pgxpool` é subpacote do `pgx` já presente; `puddle/v2` já está no grafo |

**Linha `go` permanece `1.26.0`** — verificado num módulo-amostra em `go 1.26.0` com `go get` das versões acima e `go mod tidy`: a diretiva não foi alterada. `govulncheck -test ./...` nesse módulo: `No vulnerabilities found`. A checagem que vale de fato é a repetida no repositório (task 9.1), depois que o `go.mod` real mudar. **Não vi as versões que `go mod tidy` escolherá no repositório** (o MVS pode escolher `x/text`, `x/crypto` etc. diferentes do amostra; o `x/text v0.42.0` do repositório é mais novo que o `v0.40.0` do amostra e prevalece).

Não uso `testify` (a suíte do scaffolding usa só `testing`); `testcontainers` o traz como dependência transitiva, o que é diferente de adotá-lo.

### 2.12 Fiação do `serve`

`runServe` passa a: (1) recusar `webhook` (antes de qualquer E/S); (2) abrir o `pgxpool` de `DATABASE_URL` com `Ping`; (3) construir o `telegram.Client` (o `getMe` valida o token); (4) montar `IngestMessage`; (5) rodar, num `errgroup`, o servidor HTTP existente (`serve(ctx, ln, log)` — inalterado, com seus testes) e `client.Run`; (6) ao cair o contexto, esperar os dois e só então fechar o pool. O erro de qualquer um cancela o outro. A montagem (3–6) vive numa função recebendo o `Client` já construído, para o teste ponta a ponta injetar o cliente apontado ao `tgfake` sem variável de ambiente nova.

O prazo por update é **8 s** (`ingestTimeout`), menor que os 10 s de `stop_grace_period` padrão do Compose, para um `docker compose stop` durante um update não virar `SIGKILL`. O contexto do handler é derivado com `context.WithoutCancel` do contexto raiz e limitado por esse prazo: o encerramento não aborta um `INSERT` no meio, mas nada pendura para sempre.

`worker` e `migrate` não mudam (além de a configuração exigir o token).

### 2.13 O que deliberadamente não existe ainda

- **Nenhuma outbox, nenhum despachante.** (§2.2)
- **Nenhuma `UnitOfWork` exposta ao `app`, nenhum `ports.Clock`, `Parser` ou `Resolver`.** (§2.4)
- **Nenhum `setWebhook`/`deleteWebhook`.** (§2.1)
- **Nenhum readiness check** com a query `Ping` (continua sem consumidor; `/healthz` segue sendo liveness).
- **Nenhuma instrumentação OTel.** Traço por mensagem (PRD §10) é M6. O que existe é log estruturado com `update_id`/`message_id`/`user_id`, que já permite reconstruir o caminho de uma mensagem no M1. Adicionar o SDK do OTel e um exportador OTLP é um bloco de dependências inteiro (e de superfície de `govulncheck`) que não serve ao critério de pronto do M1.
- **Nenhuma variável de ambiente nova.**

## 3. Inventário de arquivos

**Criados**

```
migrations/00002_create_users_and_messages.sql
internal/core/message/{message.go,message_test.go}
internal/core/ports/ports.go                                      (MessageStore, Notifier, SaveResult)
internal/app/{ingest_message.go,ingest_message_test.go}
internal/adapters/postgres/{pool.go,pool_test.go,messages.go,messages_test.go}
internal/adapters/postgres/queries/{users.sql,messages.sql}
internal/adapters/postgres/db/{users.sql.go,messages.sql.go}      (gerados)
internal/adapters/telegram/{client.go,update.go,notifier.go,errors.go}
internal/adapters/telegram/{update_test.go,client_test.go,notifier_test.go,privacy_test.go}
internal/testutil/testdb/{testdb.go,testdb_test.go}
internal/testutil/tgfake/tgfake.go
internal/testutil/notifiertest/notifiertest.go
internal/platform/migrations/migrations_integration_test.go
cmd/noto/ingest.go                                                 (fiação testável, §2.12 passos 3–6)
cmd/noto/ingest_test.go                                            (ponta a ponta automático)
```

**Modificados**

```
go.mod, go.sum                                   dependências (§2.11)
internal/platform/config/{config.go,config_test.go}      token obrigatório
internal/adapters/postgres/db/models.go          regenerado (User, Message)
internal/adapters/telegram/doc.go, internal/core/ports/doc.go, internal/app/doc.go   texto atualizado
cmd/noto/serve.go, cmd/noto/serve_test.go        fiação; recusa do webhook
cmd/noto/main_test.go, cmd/noto/migrate_test.go  passam a definir TELEGRAM_BOT_TOKEN
migrations/embed_test.go                          "sem CREATE TABLE" passa a valer só para a 00001
internal/core/boundary_test.go                    estende a guarda a internal/app/...
.env.example                                      só um comentário acima de TELEGRAM_BOT_TOKEN
README.md                                         instruções e status do M1 (task 10.2, humana)
```

**Não tocados:** `docs/**`, `docker-compose.yml`, `Dockerfile`, `Makefile`, `sqlc.yaml` (o glob de queries e schema já cobre os novos arquivos), `.github/workflows/ci.yml`.

O `.env` da raiz contém o token real e **nunca é lido nem citado** por qualquer task; todo passo que precisa dele o descreve por nome de variável e o executa o usuário.
