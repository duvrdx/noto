// Package postgres reúne os adaptadores de persistência sobre Postgres.
//
// No M1: MessageStore, que implementa ports.MessageStore. As queries são SQL
// escrito à mão em queries/ e o código gerado pelo sqlc vive em db/
// (versionado; regenere com sqlc generate, nunca edite). Repositório, fila e
// outbox dos marcos seguintes entram aqui quando houver quem os use.
package postgres
