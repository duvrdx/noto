-- O DO UPDATE sem efeito existe só para o RETURNING devolver o id também no
-- conflito (com DO NOTHING o Postgres não devolve a linha existente).
-- name: UpsertUser :one
INSERT INTO users (telegram_user_id)
VALUES ($1)
ON CONFLICT (telegram_user_id) DO UPDATE SET telegram_user_id = EXCLUDED.telegram_user_id
RETURNING id;
