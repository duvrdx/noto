// Package app reúne os casos de uso: orquestram o domínio e as portas de
// internal/core/ports, sem conhecer infraestrutura (nada de pgx, Telegram nem
// internal/adapters).
//
// No M1 existe IngestMessage: persistir a mensagem recebida, de forma
// idempotente em telegram_update_id, e responder com o eco. Os logs dos
// casos de uso só levam identificadores, nunca o texto do usuário.
package app
