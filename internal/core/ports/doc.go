// Package ports declara as interfaces que o domínio e os casos de uso usam
// para falar com o mundo de fora. No M1 existem duas: MessageStore e
// Notifier. As demais (Parser, Resolver, Clock) nascem com quem as consome.
package ports
