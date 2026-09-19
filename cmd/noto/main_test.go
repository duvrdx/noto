package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/duvrdx/noto/internal/platform/config"
)

// fakes monta um cli com comandos falsos que registram as chamadas e um
// loadConfig que registra se foi consultado. Nada aqui lê o ambiente real.
type fakes struct {
	cli        cli
	stdout     bytes.Buffer
	stderr     bytes.Buffer
	loadCalls  int
	cfg        config.Config
	loadErr    error
	called     []string
	gotCfg     config.Config
	commandErr error
}

func newFakes() *fakes {
	f := &fakes{cfg: config.Config{LogLevel: "info", DatabaseURL: "postgres://localhost/noto"}}
	run := func(name string) command {
		return func(ctx context.Context, cfg config.Config, log *slog.Logger) error {
			f.called = append(f.called, name)
			f.gotCfg = cfg
			return f.commandErr
		}
	}
	f.cli = cli{
		stdout: &f.stdout,
		stderr: &f.stderr,
		loadConfig: func() (config.Config, error) {
			f.loadCalls++
			return f.cfg, f.loadErr
		},
		commands: map[string]command{
			"serve":   run("serve"),
			"worker":  run("worker"),
			"migrate": run("migrate"),
		},
	}
	return f
}

func TestRunWithoutSubcommandPrintsUsageAndFails(t *testing.T) {
	f := newFakes()

	code := f.cli.run(context.Background(), nil)

	if code == 0 {
		t.Fatal("código de saída = 0, want != 0")
	}
	out := f.stderr.String()
	for _, want := range []string{"serve", "worker", "migrate"} {
		if !strings.Contains(out, want) {
			t.Errorf("uso em stderr não cita %q:\n%s", want, out)
		}
	}
	if f.stdout.Len() != 0 {
		t.Errorf("stdout deveria ficar vazio, veio: %s", f.stdout.String())
	}
}

func TestRunUnknownSubcommandPrintsUsageAndFails(t *testing.T) {
	f := newFakes()

	code := f.cli.run(context.Background(), []string{"frobnicate"})

	if code == 0 {
		t.Fatal("código de saída = 0, want != 0")
	}
	out := f.stderr.String()
	if !strings.Contains(out, "frobnicate") {
		t.Errorf("stderr deveria citar o subcomando desconhecido:\n%s", out)
	}
	for _, want := range []string{"serve", "worker", "migrate"} {
		if !strings.Contains(out, want) {
			t.Errorf("uso em stderr não cita %q:\n%s", want, out)
		}
	}
	if len(f.called) != 0 {
		t.Errorf("nenhum comando deveria rodar, rodaram: %v", f.called)
	}
}

func TestRunExtraArgumentsAreRejected(t *testing.T) {
	f := newFakes()

	code := f.cli.run(context.Background(), []string{"serve", "--porta", "9090"})

	if code == 0 {
		t.Fatal("código de saída = 0, want != 0")
	}
	if len(f.called) != 0 {
		t.Errorf("nenhum comando deveria rodar, rodaram: %v", f.called)
	}
	if !strings.Contains(f.stderr.String(), "--porta") {
		t.Errorf("stderr deveria citar o argumento extra:\n%s", f.stderr.String())
	}
}

// Uso e erro de subcomando não podem exigir configuração: o despacho vem
// antes da carga, então o uso aparece mesmo num shell sem DATABASE_URL.
func TestRunUsageErrorsNeverLoadConfig(t *testing.T) {
	for _, args := range [][]string{nil, {"frobnicate"}, {"serve", "extra"}} {
		f := newFakes()
		f.loadErr = errors.New("DATABASE_URL: obrigatória e não definida")

		code := f.cli.run(context.Background(), args)

		if code == 0 {
			t.Errorf("args %v: código = 0, want != 0", args)
		}
		if f.loadCalls != 0 {
			t.Errorf("args %v: config.Load foi chamada %d vez(es) antes do despacho", args, f.loadCalls)
		}
		if strings.Contains(f.stderr.String(), "DATABASE_URL") {
			t.Errorf("args %v: uso foi substituído por erro de config:\n%s", args, f.stderr.String())
		}
	}
}

// O mesmo, de ponta a ponta com a fiação real (config.Load de verdade) e
// DATABASE_URL ausente do ambiente.
func TestDefaultCLIUsageAppearsWithoutDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	var stdout, stderr bytes.Buffer
	c := defaultCLI(&stdout, &stderr)

	for _, args := range [][]string{nil, {"frobnicate"}} {
		stderr.Reset()
		if code := c.run(context.Background(), args); code == 0 {
			t.Errorf("args %v: código = 0, want != 0", args)
		}
		if strings.Contains(stderr.String(), "DATABASE_URL") {
			t.Errorf("args %v: erro de config no lugar do uso:\n%s", args, stderr.String())
		}
		if !strings.Contains(stderr.String(), "serve") {
			t.Errorf("args %v: sem uso em stderr:\n%s", args, stderr.String())
		}
	}
}

func TestRunRecognizesEachSubcommand(t *testing.T) {
	for _, name := range []string{"serve", "worker", "migrate"} {
		t.Run(name, func(t *testing.T) {
			f := newFakes()

			code := f.cli.run(context.Background(), []string{name})

			if code != 0 {
				t.Fatalf("código = %d, want 0 (stderr: %s)", code, f.stderr.String())
			}
			if len(f.called) != 1 || f.called[0] != name {
				t.Errorf("comandos executados = %v, want [%s]", f.called, name)
			}
			if f.loadCalls != 1 {
				t.Errorf("config.Load chamada %d vez(es), want 1", f.loadCalls)
			}
			if f.gotCfg != f.cfg {
				t.Errorf("comando recebeu config %+v, want %+v", f.gotCfg, f.cfg)
			}
			if f.stderr.Len() != 0 {
				t.Errorf("stderr deveria ficar vazio, veio: %s", f.stderr.String())
			}
			// o modo assumido é registrado em log JSON no stdout
			if !strings.Contains(f.stdout.String(), `"modo":"`+name+`"`) {
				t.Errorf("log não registra o modo %q:\n%s", name, f.stdout.String())
			}
		})
	}
}

func TestRunConfigErrorExitsNonZeroNamingVariable(t *testing.T) {
	f := newFakes()
	f.loadErr = errors.New("configuração inválida: DATABASE_URL: obrigatória e não definida")

	code := f.cli.run(context.Background(), []string{"serve"})

	if code == 0 {
		t.Fatal("código = 0, want != 0")
	}
	if !strings.Contains(f.stderr.String(), "DATABASE_URL") {
		t.Errorf("stderr deveria citar DATABASE_URL:\n%s", f.stderr.String())
	}
	if len(f.called) != 0 {
		t.Errorf("comando não deveria rodar com config inválida: %v", f.called)
	}
}

// logging.New rejeita LOG_LEVEL inválido e config.Load não; o main é quem
// precisa propagar esse erro.
func TestRunInvalidLogLevelExitsNonZeroNamingVariable(t *testing.T) {
	f := newFakes()
	f.cfg.LogLevel = "banana"

	code := f.cli.run(context.Background(), []string{"serve"})

	if code == 0 {
		t.Fatal("código = 0, want != 0")
	}
	if !strings.Contains(f.stderr.String(), "LOG_LEVEL") {
		t.Errorf("stderr deveria citar LOG_LEVEL:\n%s", f.stderr.String())
	}
	if len(f.called) != 0 {
		t.Errorf("comando não deveria rodar: %v", f.called)
	}
}

func TestDefaultCLIInvalidLogLevelExitsNonZero(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/noto")
	t.Setenv("LOG_LEVEL", "banana")
	var stdout, stderr bytes.Buffer

	code := defaultCLI(&stdout, &stderr).run(context.Background(), []string{"serve"})

	if code == 0 {
		t.Fatal("código = 0, want != 0")
	}
	if !strings.Contains(stderr.String(), "LOG_LEVEL") {
		t.Errorf("stderr deveria citar LOG_LEVEL:\n%s", stderr.String())
	}
}

func TestRunCommandErrorExitsNonZero(t *testing.T) {
	f := newFakes()
	f.commandErr = errors.New("falha do comando")

	code := f.cli.run(context.Background(), []string{"worker"})

	if code == 0 {
		t.Fatal("código = 0, want != 0")
	}
	if !strings.Contains(f.stderr.String(), "falha do comando") {
		t.Errorf("stderr deveria trazer o erro:\n%s", f.stderr.String())
	}
}

// Encerramento gracioso não é erro: cancelamento do contexto sem falha
// do comando termina com código zero.
func TestRunCommandReturningNilAfterCancelExitsZero(t *testing.T) {
	f := newFakes()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if code := f.cli.run(ctx, []string{"worker"}); code != 0 {
		t.Errorf("código = %d, want 0", code)
	}
}
