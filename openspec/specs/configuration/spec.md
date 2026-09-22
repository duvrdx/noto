# configuration Specification

## Purpose
TBD - created by archiving change add-project-scaffolding. Update Purpose after archive.

## Requirements

### Requirement: Configuração exclusivamente por variáveis de ambiente

A aplicação SHALL obter toda a sua configuração de variáveis de ambiente, sem arquivo de configuração próprio, e SHALL reconhecer exatamente o conjunto já fixado em `.env.example`: `TELEGRAM_BOT_TOKEN`, `TELEGRAM_TRANSPORT`, `TELEGRAM_WEBHOOK_SECRET`, `OLLAMA_HOST`, `OLLAMA_API_KEY`, `OLLAMA_MODEL`, `DATABASE_URL`, `LOG_LEVEL`, `DEFAULT_TIMEZONE`, `OTEL_EXPORTER_OTLP_ENDPOINT`. Segredos SHALL vir apenas do ambiente, nunca de código (PRD §12).

#### Scenario: Configuração completa é carregada

- **Given** todas as variáveis de `.env.example` definidas no ambiente
- **When** qualquer subcomando do `noto` inicia
- **Then** a configuração é carregada numa struct única e o processo prossegue

#### Scenario: Variável não reconhecida é ignorada

- **Given** uma variável de ambiente fora do conjunto declarado
- **When** a configuração é carregada
- **Then** ela é ignorada e nenhum erro é produzido

### Requirement: Validação na inicialização

A configuração SHALL ser validada no início do processo, antes de qualquer trabalho. Variável obrigatória ausente ou valor fora do domínio permitido SHALL encerrar o processo com código diferente de zero e uma mensagem que **nomeia a variável**. `TELEGRAM_TRANSPORT` SHALL aceitar somente `polling` ou `webhook` (ADR 0008); `DEFAULT_TIMEZONE` SHALL ser um identificador IANA carregável (ADR 0005). Quando a validação reprovar mais de uma variável, a mensagem SHALL citar todas de uma vez, não apenas a primeira.

#### Scenario: Variável obrigatória ausente

- **Given** `DATABASE_URL` não definida
- **When** `noto serve` inicia
- **Then** o processo encerra com código diferente de zero e a mensagem de erro cita `DATABASE_URL`

#### Scenario: Transporte inválido

- **Given** `TELEGRAM_TRANSPORT=carrier-pigeon`
- **When** a configuração é validada
- **Then** o processo encerra com erro citando `TELEGRAM_TRANSPORT` e os valores aceitos

#### Scenario: Fuso inválido

- **Given** `DEFAULT_TIMEZONE=Mars/Olympus_Mons`
- **When** a configuração é validada
- **Then** o processo encerra com erro citando `DEFAULT_TIMEZONE`

#### Scenario: Múltiplas variáveis inválidas são reportadas juntas

- **Given** `DATABASE_URL` ausente e `TELEGRAM_TRANSPORT=carrier-pigeon` ao mesmo tempo
- **When** a configuração é validada
- **Then** o processo encerra com uma única mensagem de erro que cita as duas variáveis

### Requirement: `.env.example` é carregável sem transformação

`.env.example` SHALL ser um arquivo que possa ser copiado para `.env` e consumido tanto pelo `env_file` do Docker Compose quanto por `set -a; . ./.env` num shell POSIX, sem edição. Comentários SHALL ocupar uma linha própria acima da variável que descrevem; um comentário SHALL NOT aparecer na mesma linha depois de um valor, porque ambos os consumidores o incorporariam ao valor. Os valores declarados no arquivo SHALL apontar para `localhost`, que é o correto para executar o binário fora de contêiner.

#### Scenario: Shell carrega o arquivo sem sujar o valor

- **Given** `.env.example` no repositório
- **When** `set -a; . ./.env.example; set +a` é executado num shell limpo
- **Then** o comando encerra sem erro e `TELEGRAM_TRANSPORT` vale exatamente `polling`, sem nenhum texto de comentário anexado

#### Scenario: Compose carrega o arquivo sem sujar o valor

- **Given** um `.env` copiado de `.env.example`
- **When** `docker compose config` é executado
- **Then** a configuração resolvida é válida e nenhuma variável carrega texto de comentário no valor

### Requirement: Log estruturado em JSON

A aplicação SHALL usar `log/slog` com handler JSON para toda a saída de log (ADR 0001, PRD §8.3), com o nível vindo de `LOG_LEVEL`. O logger SHALL ser injetado nos subcomandos; pacotes SHALL NOT usar o logger global do pacote `log`.

#### Scenario: Saída é JSON no nível configurado

- **Given** `LOG_LEVEL=info`
- **When** o processo registra um evento de inicialização
- **Then** a linha emitida é um objeto JSON válido com nível, timestamp e mensagem

#### Scenario: Nível abaixo do configurado é suprimido

- **Given** `LOG_LEVEL=info`
- **When** um evento de nível `debug` é registrado
- **Then** nada é emitido

### Requirement: Segredos nunca aparecem em log

Nem a configuração nem qualquer log de inicialização SHALL emitir os valores de `TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEBHOOK_SECRET`, `OLLAMA_API_KEY` ou a senha embutida em `DATABASE_URL` (PRD §12). Se a struct de configuração for formatada para log, os campos sensíveis SHALL ser redigidos.

#### Scenario: Config logada na inicialização é redigida

- **Given** `TELEGRAM_BOT_TOKEN` com um valor qualquer
- **When** a configuração carregada é registrada em log na inicialização
- **Then** o valor do token não aparece na saída, e em seu lugar há um marcador de redação

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
