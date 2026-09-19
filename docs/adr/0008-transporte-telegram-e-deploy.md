# ADR 0008 — Transporte do Telegram e alvo de deploy

- **Status:** Aceito
- **Data:** 2026-09-19

## Contexto

O webhook do Telegram exige **HTTPS público com certificado válido**. Isso cria um problema que aparece cedo e atrapalha todo o resto: não dá para rodar o projeto na máquina de desenvolvimento sem um túnel (ngrok e equivalentes), o que significa uma dependência externa, uma URL que muda a cada reinício e uma reconfiguração do bot a cada sessão.

Como o M1 é justamente "receber uma mensagem e responder", essa fricção atinge o primeiro marco e se paga em toda sessão de desenvolvimento daí em diante.

Em paralelo, o alvo de deploy nunca foi escolhido, e ele determina como o webhook é exposto.

## Decisão

### 1. Dois transportes, mesmo caso de uso

O adapter Telegram suporta ambos, selecionados por configuração:

| Ambiente | Transporte | Motivo |
|---|---|---|
| Desenvolvimento | long polling (`getUpdates`) | Nenhuma exposição de rede, nenhum túnel |
| Produção | webhook + `secret_token` | Menor latência, sem polling constante |

A fronteira está **dentro** do adapter: os dois caminhos produzem o mesmo `Update` e chamam o mesmo caso de uso `IngestMessage`. Nada acima do adapter sabe qual transporte está em uso, e nenhum teste precisa saber.

### 2. Idempotência é do domínio, não do transporte

`telegram_update_id UNIQUE` protege os dois caminhos igualmente. Long polling sem confirmação de offset reentrega updates, assim como o webhook reentrega sem ACK — a proteção é a mesma porque o problema é o mesmo. Isso importa mais do que pareceria, porque o sistema executa ações sem confirmação prévia ([ADR 0006](./0006-confirmacao-seletiva-e-desfazer.md)): um update reprocessado é uma ação duplicada na agenda do usuário.

### 3. Alvo de deploy: VPS única com Docker Compose e proxy reverso

Um host, `docker compose up`, proxy reverso à frente terminando TLS com certificado automático.

Isso mantém produção e desenvolvimento com **a mesma topologia** — os mesmos contêineres, na mesma rede, com a mesma configuração — diferindo apenas no transporte e nos segredos. Quanto menor a distância entre os dois ambientes, menos bugs existem apenas em um deles.

## Alternativas consideradas

**Só webhook, com túnel em desenvolvimento.** Rejeitado pelo atrito diário: URL instável, reconfiguração do bot a cada sessão e uma dependência de terceiro no caminho de quem só quer rodar o projeto. Para um repositório de portfólio há um custo adicional — "clone e rode" deixa de ser verdade.

**Só long polling, inclusive em produção.** Simplifica o deploy a ponto de dispensar HTTPS. Rejeitado: polling contínuo desperdiça requisições ociosas, adiciona latência ao caminho que o §9 do PRD orçou em 400 ms, e descarta a autenticação por `secret_token` que o webhook oferece de graça. Permanece como plano de contingência caso a exposição HTTPS fique indisponível.

**PaaS (Fly.io, Railway, Render).** TLS e deploy resolvidos, menos operação. Rejeitado para o MVP por duas razões: remove o aprendizado de operação que é objetivo declarado do projeto (§1.2 do PRD), e afasta produção do ambiente local, que é a distância onde bugs se escondem. Continua sendo a saída natural se a operação da VPS virar um peso — nada na arquitetura impede.

**Kubernetes.** Desproporcional a dois contêineres e um Postgres.

## Consequências

**Aceitas como positivas**

- `docker compose up` mais um token de bot bastam para desenvolver. Sem túnel, sem domínio, sem conta em terceiro.
- Produção e desenvolvimento compartilham topologia.
- O transporte vira detalhe substituível; migrar para um PaaS depois não toca o domínio.

**Aceitas como custo**

- **Dois caminhos de código no adapter**, e portanto dois caminhos para manter e testar. Mitigado por convergirem imediatamente para o mesmo `Update` — a divergência é de dezenas de linhas, não de arquitetura.
- **Um bug pode existir só em produção**, se o webhook tiver comportamento que o polling não reproduz. Mitigado pelo teste de contrato do adapter, que exercita os dois transportes contra o mesmo conjunto de updates (ver [`testing.md`](../testing.md)).
- **Operação da VPS é responsabilidade nossa**: certificado, atualizações, backup do Postgres. É custo real, e é parcialmente a intenção — o aprendizado está aí.
