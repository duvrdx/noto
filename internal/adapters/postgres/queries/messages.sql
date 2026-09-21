-- No conflito de telegram_update_id nenhuma linha é devolvida (pgx.ErrNoRows
-- no :one): é assim que o chamador sabe que o update já estava gravado.
-- name: InsertMessage :one
INSERT INTO messages (user_id, telegram_update_id, chat_id, raw_text)
VALUES ($1, $2, $3, $4)
ON CONFLICT (telegram_update_id) DO NOTHING
RETURNING id;
