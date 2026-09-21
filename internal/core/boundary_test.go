package core

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const mod = "github.com/duvrdx/noto"

func TestIsForbidden(t *testing.T) {
	forbidden := []string{
		"github.com/jackc/pgx",
		"github.com/jackc/pgx/v5",
		"github.com/jackc/pgx/v5/stdlib",
		"github.com/go-telegram/bot",
		"github.com/go-telegram/bot/models",
		"github.com/go-telegram-bot-api/telegram-bot-api/v5",
		"github.com/mymmrac/telego",
		"github.com/tucnak/telebot",
		"ollama",
		"github.com/ollama/ollama/api",
		"github.com/algum/ollama",
		mod + "/internal/adapters/ollama",
		mod + "/internal/adapters",
		mod + "/internal/adapters/postgres",
		mod + "/internal/adapters/postgres/db",
	}
	for _, p := range forbidden {
		if !isForbidden(p) {
			t.Errorf("isForbidden(%q) = false, want true", p)
		}
	}

	allowed := []string{
		"fmt",
		"net/http",
		"database/sql",
		mod + "/internal/core/item",
		mod + "/internal/platform/migrations",
		"github.com/jackc/pgxfoo",        // prefixo por elemento, não por substring
		"github.com/algum/ollamafoo",     // falso positivo: não é o elemento ollama
		"github.com/algum/foo-ollama",    // idem
		"github.com/algum/ollama-go",     // idem
		"github.com/go-telegram/botfoo",  // idem
		mod + "/internal/adaptersfoo",    // idem
		"github.com/duvrdx/noto-other/x", // outro módulo
	}
	for _, p := range allowed {
		if isForbidden(p) {
			t.Errorf("isForbidden(%q) = true, want false", p)
		}
	}
}

func chainStrings(vs []violation) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = strings.Join(v.chain, " -> ")
	}
	return out
}

func TestFindViolationsCleanTreePasses(t *testing.T) {
	g := importGraph{
		mod + "/internal/core/a": {mod + "/internal/core/b", "fmt", "time"},
		mod + "/internal/core/b": {"errors"},
	}

	if vs := findViolations(g, []string{mod + "/internal/core/a", mod + "/internal/core/b"}); len(vs) != 0 {
		t.Errorf("árvore limpa não deveria violar: %v", chainStrings(vs))
	}
}

func TestFindViolationsDirectInfraImportFails(t *testing.T) {
	g := importGraph{
		mod + "/internal/core/a": {"fmt", "github.com/jackc/pgx/v5"},
	}

	vs := findViolations(g, []string{mod + "/internal/core/a"})

	got := chainStrings(vs)
	want := mod + "/internal/core/a -> github.com/jackc/pgx/v5"
	if len(got) != 1 || got[0] != want {
		t.Errorf("violações = %v, want [%s]", got, want)
	}
	if vs[0].root != mod+"/internal/core/a" || vs[0].forbidden != "github.com/jackc/pgx/v5" {
		t.Errorf("violação deveria nomear o pacote e o import proibido: %+v", vs[0])
	}
}

func TestFindViolationsTransitiveReportsFullChain(t *testing.T) {
	g := importGraph{
		mod + "/internal/core/a": {mod + "/internal/core/b"},
		mod + "/internal/core/b": {"github.com/go-telegram/bot"},
	}

	got := chainStrings(findViolations(g, []string{mod + "/internal/core/a", mod + "/internal/core/b"}))

	want := []string{
		mod + "/internal/core/a -> " + mod + "/internal/core/b -> github.com/go-telegram/bot",
		mod + "/internal/core/b -> github.com/go-telegram/bot",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("cadeias = %v, want %v", got, want)
	}
}

func TestFindViolationsChainThroughNonCorePackage(t *testing.T) {
	g := importGraph{
		mod + "/internal/core/item":           {mod + "/internal/platform/migrations"},
		mod + "/internal/platform/migrations": {"database/sql", "github.com/jackc/pgx/v5/stdlib"},
		"github.com/jackc/pgx/v5/stdlib":      {"github.com/jackc/pgx/v5"},
	}

	got := chainStrings(findViolations(g, []string{mod + "/internal/core/item"}))

	want := mod + "/internal/core/item -> " + mod + "/internal/platform/migrations -> github.com/jackc/pgx/v5/stdlib"
	if len(got) != 1 || got[0] != want {
		t.Errorf("cadeias = %v, want [%s]", got, want)
	}
}

func TestFindViolationsCoreImportingAdaptersFails(t *testing.T) {
	g := importGraph{
		mod + "/internal/core/a":            {mod + "/internal/adapters/postgres"},
		mod + "/internal/adapters/postgres": {"github.com/jackc/pgx/v5"},
	}

	got := chainStrings(findViolations(g, []string{mod + "/internal/core/a"}))

	// para no primeiro proibido do caminho: adapters, não o pgx que vem depois
	want := mod + "/internal/core/a -> " + mod + "/internal/adapters/postgres"
	if len(got) != 1 || got[0] != want {
		t.Errorf("cadeias = %v, want [%s]", got, want)
	}
}

func TestFindViolationsReportsShortestChainAndSurvivesCycles(t *testing.T) {
	g := importGraph{
		mod + "/internal/core/a": {mod + "/internal/core/b", mod + "/internal/core/c"},
		mod + "/internal/core/b": {mod + "/internal/core/a", mod + "/internal/core/c"}, // ciclo a <-> b
		mod + "/internal/core/c": {"github.com/jackc/pgx/v5"},
	}

	got := chainStrings(findViolations(g, []string{mod + "/internal/core/a"}))

	want := mod + "/internal/core/a -> " + mod + "/internal/core/c -> github.com/jackc/pgx/v5"
	if len(got) != 1 || got[0] != want {
		t.Errorf("cadeias = %v, want [%s]", got, want)
	}
}

func TestFindViolationsReportsEachForbiddenImportOfARoot(t *testing.T) {
	g := importGraph{
		mod + "/internal/core/a": {"github.com/jackc/pgx/v5", "github.com/ollama/ollama/api"},
	}

	got := chainStrings(findViolations(g, []string{mod + "/internal/core/a"}))

	if len(got) != 2 {
		t.Errorf("esperava 2 violações (pgx e ollama), veio %v", got)
	}
}

// ---------------------------------------------------------------------------
// Checker: função pura sobre o grafo de imports (testada acima com grafos
// sintéticos) e o teste de integração sobre a árvore real (abaixo).
// ---------------------------------------------------------------------------

// forbiddenPrefixes proíbe o pacote e tudo abaixo dele, por elemento de
// caminho: "github.com/jackc/pgx" casa "github.com/jackc/pgx/v5/stdlib" mas
// não "github.com/jackc/pgxfoo".
var forbiddenPrefixes = []string{
	"github.com/jackc/pgx",       // driver de banco
	"github.com/go-telegram/bot", // clientes de Telegram (o do ADR 0008 e os comuns)
	"github.com/go-telegram-bot-api",
	"github.com/mymmrac/telego",
	"github.com/tucnak/telebot",
	"github.com/duvrdx/noto/internal/adapters", // core nunca importa adapters
}

// forbiddenElement proíbe qualquer pacote com este elemento de caminho:
// cobre o cliente de Ollama seja qual for o módulo que o publique.
const forbiddenElement = "ollama"

// isForbidden diz se importar path viola a fronteira. A stdlib nunca viola.
func isForbidden(path string) bool {
	for _, p := range forbiddenPrefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return slices.Contains(strings.Split(path, "/"), forbiddenElement)
}

// importGraph mapeia o caminho de um pacote aos caminhos que ele importa
// diretamente.
type importGraph map[string][]string

// violation é um caminho de imports de um pacote de core até o primeiro
// import proibido.
type violation struct {
	root      string   // pacote de core violador
	forbidden string   // import proibido alcançado
	chain     []string // root -> ... -> forbidden
}

// findViolations percorre o grafo em largura a partir de cada root e devolve,
// para cada import proibido alcançável, a cadeia mais curta. Não atravessa um
// pacote proibido: reporta o primeiro proibido do caminho (adapters/postgres,
// não o pgx que ele importa). Determinístico: raízes e imports em ordem.
func findViolations(g importGraph, roots []string) []violation {
	roots = slices.Clone(roots)
	sort.Strings(roots)

	var out []violation
	for _, root := range roots {
		parent := map[string]string{root: ""}
		queue := []string{root}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]

			imports := slices.Clone(g[cur])
			sort.Strings(imports)
			for _, imp := range imports {
				if _, seen := parent[imp]; seen {
					continue
				}
				parent[imp] = cur
				if isForbidden(imp) {
					out = append(out, violation{root: root, forbidden: imp, chain: chainTo(parent, imp)})
					continue // não atravessa o proibido
				}
				queue = append(queue, imp)
			}
		}
	}
	return out
}

func chainTo(parent map[string]string, node string) []string {
	var rev []string
	for n := node; n != ""; n = parent[n] {
		rev = append(rev, n)
	}
	slices.Reverse(rev)
	return rev
}

// moduleRoot acha o diretório do go.mod subindo a partir deste arquivo, sem
// depender do diretório de trabalho do go test.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller falhou")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod não encontrado subindo de %s", filepath.Dir(file))
		}
		dir = parent
	}
}

// Este arquivo mora em internal/core, mas guarda também internal/app: a
// fronteira do ADR 0001 (PRD §8.2) vale para o domínio e para os casos de uso,
// e um segundo teste de fronteira, com a mesma lista de proibições, seria uma
// segunda cópia dela para manter. Casos de uso falam com o exterior só por
// portas (internal/core/ports).

// TestCoreDoesNotDependOnInfrastructure carrega a árvore real de
// ./internal/core/... e falha se qualquer pacote, direta ou transitivamente,
// importar infraestrutura, reportando a cadeia completa.
//
// Tests: true, então os _test.go de core também entram: o domínio é puro
// inclusive nos testes.
func TestCoreDoesNotDependOnInfrastructure(t *testing.T) {
	checkBoundary(t, "./internal/core/...", true)
}

// TestAppDoesNotDependOnInfrastructure faz o mesmo para ./internal/app/...,
// mas só com o código de produção (Tests: false): os _test.go de app podem
// importar adapters para montar o cenário (por exemplo o repositório real do
// Postgres), e a spec architecture-boundary diz que isso passa.
func TestAppDoesNotDependOnInfrastructure(t *testing.T) {
	checkBoundary(t, "./internal/app/...", false)
}

// checkBoundary carrega os pacotes de pattern e falha se algum importar,
// direta ou transitivamente, um pacote proibido. Com tests, as variantes de
// teste dos pacotes entram; elas são unidas por caminho de import, e o binário
// sintético "*.test" é ignorado como raiz.
func checkBoundary(t *testing.T, pattern string, tests bool) {
	t.Helper()
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedImports | packages.NeedDeps,
		Dir:   moduleRoot(t),
		Tests: tests,
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}

	graph := importGraph{}
	var roots []string
	seenRoot := map[string]bool{}
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			t.Errorf("erro ao carregar %s: %v", p.ID, e)
		}
		for _, imp := range p.Imports {
			if !slices.Contains(graph[p.PkgPath], imp.PkgPath) {
				graph[p.PkgPath] = append(graph[p.PkgPath], imp.PkgPath)
			}
		}
	})
	for _, p := range pkgs {
		if strings.HasSuffix(p.PkgPath, ".test") || seenRoot[p.PkgPath] {
			continue
		}
		seenRoot[p.PkgPath] = true
		roots = append(roots, p.PkgPath)
	}
	if len(roots) == 0 {
		t.Fatalf("nenhum pacote carregado sob %s: o teste passaria no vazio", pattern)
	}

	for _, v := range findViolations(graph, roots) {
		t.Errorf("%s importa %s, proibido na fronteira (%s):\n    %s",
			v.root, v.forbidden, pattern, strings.Join(v.chain, "\n      -> "))
	}
}
