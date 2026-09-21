// Package telegram é o adaptador do Telegram (ADR 0008). No M1 recebe por
// long polling: Client consulta getUpdates, descarta o que não é texto de chat
// privado (MapUpdate) e entrega o resto, serial e em ordem, a um Handler.
//
// O adaptador nunca escreve no pacote log global e nunca registra o texto de
// uma mensagem nem o token: os logs levam update_id e um motivo ou uma
// classificação. A biblioteca go-telegram/bot é configurada de modo que seus
// manipuladores padrão, que vazariam ambos, sejam substituídos.
package telegram
