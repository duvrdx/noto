-- +goose Up
-- pg_trgm: similaridade de texto para a resolução de referência (ADR 0007).
-- Nenhuma tabela do PRD §6.2 aqui; elas vêm com o código que as usa.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- +goose Down
DROP EXTENSION IF EXISTS pg_trgm;
