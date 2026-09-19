// Comando noto: binário único com os modos serve, worker e migrate
// (ADR 0001, PRD §8.1).
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/duvrdx/noto/internal/platform/config"
	"github.com/duvrdx/noto/internal/platform/logging"
)

// Códigos de saída: 0 sucesso (inclusive encerramento gracioso), 1 falha em
// tempo de execução, 2 erro de uso.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// command é a assinatura comum dos subcomandos. O context é a única forma de
// encerramento: quando ele cai, o comando para e devolve nil.
type command func(ctx context.Context, cfg config.Config, log *slog.Logger) error

// cli reúne as dependências do despacho, para que ele seja testável sem
// tocar no ambiente, no relógio nem em processo real.
type cli struct {
	stdout     io.Writer // logs
	stderr     io.Writer // uso e erros
	loadConfig func() (config.Config, error)
	commands   map[string]command
}

// defaultCLI é a fiação de produção.
func defaultCLI(stdout, stderr io.Writer) cli {
	return cli{
		stdout:     stdout,
		stderr:     stderr,
		loadConfig: config.Load,
		commands: map[string]command{
			"serve":   runServe,
			"worker":  runWorker,
			"migrate": runMigrate,
		},
	}
}

// main é o único ponto de saída do processo: monta o context raiz, delega e
// traduz o resultado em código de saída.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := defaultCLI(os.Stdout, os.Stderr).run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

// run despacha args (sem o nome do programa) e devolve o código de saída.
//
// A ordem importa: subcomando ausente, desconhecido ou com argumentos
// sobrando é recusado antes de qualquer config.Load, para que o uso apareça
// mesmo num shell sem DATABASE_URL.
func (c cli) run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		c.usage("")
		return exitUsage
	}
	name := args[0]
	cmd, ok := c.commands[name]
	if !ok {
		c.usage(fmt.Sprintf("subcomando desconhecido %q", name))
		return exitUsage
	}
	if len(args) > 1 {
		c.usage(fmt.Sprintf("%s: argumentos inesperados: %s", name, strings.Join(args[1:], " ")))
		return exitUsage
	}

	cfg, err := c.loadConfig()
	if err != nil {
		fmt.Fprintf(c.stderr, "noto %s: %v\n", name, err)
		return exitError
	}
	log, err := logging.New(c.stdout, cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(c.stderr, "noto %s: %v\n", name, err)
		return exitError
	}

	log.Info("iniciando", "modo", name, "config", cfg)
	if err := cmd(ctx, cfg, log); err != nil {
		fmt.Fprintf(c.stderr, "noto %s: %v\n", name, err)
		return exitError
	}
	log.Info("encerrado", "modo", name)
	return exitOK
}

// usage escreve o uso em stderr, com o motivo quando houver.
func (c cli) usage(reason string) {
	if reason != "" {
		fmt.Fprintf(c.stderr, "noto: %s\n\n", reason)
	}
	names := make([]string, 0, len(c.commands))
	for n := range c.commands {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintf(c.stderr, "uso: noto <subcomando>\n\nsubcomandos:\n")
	for _, n := range names {
		fmt.Fprintf(c.stderr, "  %s\n", n)
	}
}
