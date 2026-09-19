# ADR 0005 — Modelo temporal: UTC no banco, IANA por usuário

- **Status:** Aceito
- **Data:** 2026-09-19

## Contexto

Todo o valor do Noto depende de acertar **quando**. "Amanhã às 14h" só tem sentido em relação a um instante e a um fuso. Erros de fuso neste produto não são bugs cosméticos: são lembretes que chegam na hora errada, que é o mesmo que não chegar.

Complicadores reais: o Brasil já teve e pode voltar a ter horário de verão; usuários viajam; "amanhã" depende do fuso de quem fala, não do servidor.

## Decisão

### 1. Armazenamento sempre em UTC

Todo instante no banco é `timestamptz` normalizado em UTC. Nenhuma coluna guarda hora local.

### 2. Fuso IANA por usuário, capturado no onboarding

`users.timezone` guarda o identificador IANA (ex.: `America/Sao_Paulo`), **nunca** um offset fixo como `-03:00`. Offset fixo quebra em mudança de horário de verão; identificador IANA carrega as regras.

A captura acontece no `/start` e é confirmada explicitamente, com default `America/Sao_Paulo`. Sem fuso não há produto.

### 3. Conversão só nas bordas

O domínio opera em UTC. A conversão para hora local acontece em dois pontos, e apenas neles:

- **entrada** — o `now` enviado ao parser já está no fuso do usuário;
- **saída** — a renderização de qualquer data numa mensagem.

### 4. Contrato temporal com o modelo

O prompt recebe `now` formatado no fuso do usuário e o nome IANA. O modelo devolve ISO-8601 **com offset explícito**. Um instante sem offset é rejeitado — é ambíguo por definição.

### 5. Revalidação determinística

Independentemente do que o modelo devolveu:

- data no passado → rejeitada (com exceção de janela curta de tolerância para "hoje às 9h" dito às 9h05);
- mais de dois anos no futuro → rejeitada (sintoma clássico de ano alucinado);
- offset inconsistente com o fuso IANA na data em questão → rejeitada.

### 6. Relógio injetável

O domínio nunca chama `time.Now()` diretamente. Um `ports.Clock` é injetado, o que torna testável o comportamento em horário de verão, virada de ano e virada de dia sem depender do relógio da máquina.

### 7. "Hoje" é do usuário

`/hoje` e `/semana` computam suas janelas no fuso do usuário, convertidas para UTC antes da query. O fuso do servidor é irrelevante em qualquer lugar do sistema — e o container roda em UTC para que qualquer vazamento dessa suposição apareça cedo.

## Alternativas consideradas

**Guardar hora local com offset por registro.** Sobrevive à mudança de fuso da agenda, mas torna qualquer ordenação ou comparação entre registros uma operação delicada, e complica os índices da fila de lembretes.

**Um único fuso fixo para todos.** Simples até o primeiro usuário fora dele, ou o primeiro que viaja. Barato demais de fazer certo para justificar fazer errado.

**Deixar o modelo resolver datas sem revalidação.** Rejeitado — ADR 0002 trata o modelo como não confiável, e data é justamente o campo onde alucinação é mais provável e mais custosa.

## Consequências

**Aceitas como positivas**

- Horário de verão e viagem de usuário são tratados pelas regras IANA, não por código ad hoc.
- Comparação, ordenação e indexação de instantes são triviais — tudo é UTC.
- `Clock` injetável torna casos de borda temporal testáveis de forma determinística.

**Aceitas como custo**

- Conversão explícita nas bordas é cerimônia que precisa de disciplina — esquecer uma produz erro sutil e difícil de ver em revisão.
- Um item criado antes de o usuário mudar de fuso mantém o instante absoluto, não a hora local. É o comportamento correto para um compromisso com outra pessoa, e potencialmente surpreendente para um lembrete pessoal. Fora do escopo do MVP; revisitar se aparecer na prática.
