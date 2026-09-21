-- +goose Up
-- M1: só as colunas que o marco lê ou escreve (PRD §6.2 é o alvo final; cada
-- coluna omitida chega com o marco que a usa). Sem users.timezone/locale,
-- sem messages.telegram_message_id, sem índices extras e sem ON DELETE CASCADE.
CREATE TABLE users (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    telegram_user_id bigint      NOT NULL UNIQUE,
    created_at       timestamptz NOT NULL DEFAULT now()
);

-- telegram_update_id é UNIQUE e NOT NULL: é a chave de idempotência da
-- entrada, e um NULL contornaria o UNIQUE (NULLs são distintos no Postgres).
CREATE TABLE messages (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            uuid        NOT NULL REFERENCES users (id),
    telegram_update_id bigint      NOT NULL UNIQUE,
    chat_id            bigint      NOT NULL,
    raw_text           text        NOT NULL,
    received_at        timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
-- messages antes de users (a FK). pg_trgm é da 00001 e não é tocada aqui.
DROP TABLE messages;
DROP TABLE users;
