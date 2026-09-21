# configuration

## REMOVED Requirements

### Requirement: No scaffolding, apenas `DATABASE_URL` é obrigatória

**Reason**: o requirement foi escrito para o scaffolding, quando ainda não existia adapter de Telegram que consumisse `TELEGRAM_BOT_TOKEN`; ele mesmo dizia que o token "SHALL se tornar obrigatório no M1". No M1 o adapter existe, e um `serve` sem token só falharia na primeira chamada de rede. Dois de seus cenários ("`.env` recém-copiado sobe o serve" e "Token de Telegram vazio não é erro") passam a ser falsos, por isso o requirement é removido em vez de modificado: um `MODIFIED` teria de carregar de volta cenários que contradizem a nova regra.

**Migration**: substituído pelo requirement "Variáveis obrigatórias", adicionado abaixo, que torna `TELEGRAM_BOT_TOKEN` obrigatório para todos os subcomandos e mantém as demais regras (`DATABASE_URL` obrigatória; `TELEGRAM_WEBHOOK_SECRET` obrigatória apenas sob `TELEGRAM_TRANSPORT=webhook`; `OLLAMA_API_KEY` e `OLLAMA_MODEL` aceitas vazias).

## ADDED Requirements

### Requirement: Variáveis obrigatórias

As variáveis incondicionalmente obrigatórias SHALL ser `DATABASE_URL` e `TELEGRAM_BOT_TOKEN`. Uma variável obrigatória definida como string vazia ou composta apenas de espaços SHALL ser tratada como ausente. `TELEGRAM_WEBHOOK_SECRET` SHALL ser obrigatória apenas quando `TELEGRAM_TRANSPORT` for `webhook` (ADR 0008). Todas as demais variáveis do conjunto — incluindo `OLLAMA_API_KEY` e `OLLAMA_MODEL` — SHALL continuar aceitas vazias, e um valor vazio nelas SHALL NOT impedir nenhum subcomando de iniciar.

A obrigatoriedade de `TELEGRAM_BOT_TOKEN` SHALL valer para **todos** os subcomandos (`serve`, `worker` e `migrate`), porque a configuração é carregada e validada uma única vez antes do despacho. A mensagem de erro SHALL nomear a variável e SHALL NOT conter o valor de nenhum segredo.

#### Scenario: Token ausente reprova

- **Given** `TELEGRAM_BOT_TOKEN` não definida e um `DATABASE_URL` válido
- **When** `noto serve` inicia
- **Then** o processo encerra com código diferente de zero e a mensagem de erro cita `TELEGRAM_BOT_TOKEN`

#### Scenario: Token vazio ou só com espaços reprova

- **Given** `TELEGRAM_BOT_TOKEN` definida como string vazia, ou como espaços
- **When** a configuração é validada
- **Then** o processo encerra com erro citando `TELEGRAM_BOT_TOKEN`

#### Scenario: Token e banco ausentes são reportados juntos

- **Given** `TELEGRAM_BOT_TOKEN` e `DATABASE_URL` ambas ausentes
- **When** a configuração é validada
- **Then** uma única mensagem de erro cita as duas variáveis

#### Scenario: `.env.example` copiado sem edição é recusado

- **Given** um `.env` copiado literalmente de `.env.example`, portanto com `TELEGRAM_BOT_TOKEN` vazio
- **When** `noto serve`, `noto worker` ou `noto migrate` inicia
- **Then** o processo encerra com erro citando `TELEGRAM_BOT_TOKEN`, antes de qualquer trabalho

#### Scenario: `.env` com o token preenchido carrega

- **Given** um `.env` copiado de `.env.example` com `TELEGRAM_BOT_TOKEN` preenchido, `DATABASE_URL` válido e `TELEGRAM_WEBHOOK_SECRET`, `OLLAMA_API_KEY` e `OLLAMA_MODEL` vazios
- **When** a configuração é validada
- **Then** nenhum erro é produzido e o processo prossegue

#### Scenario: Webhook sem segredo continua reprovando

- **Given** `TELEGRAM_TRANSPORT=webhook` e `TELEGRAM_WEBHOOK_SECRET` vazio
- **When** a configuração é validada
- **Then** o processo encerra com erro citando `TELEGRAM_WEBHOOK_SECRET`
