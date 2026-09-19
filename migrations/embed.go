// Package migrations embute as migrações SQL do goose no binário.
//
// O embed vive aqui, ao lado dos .sql, porque //go:embed não alcança
// diretórios acima do pacote. Quem aplica as migrações é
// internal/platform/migrations; o sqlc lê estes mesmos arquivos como schema.
package migrations

import "embed"

// FS contém as migrações *.sql na raiz, prontas para o goose.
//
//go:embed *.sql
var FS embed.FS
