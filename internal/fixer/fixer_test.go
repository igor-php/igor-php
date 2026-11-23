package fixer

import (
	"strings"
	"testing"

	"github.com/igor-php/igor-php/internal/analyzer"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// audit runs the real analyzer so tests exercise the exact finding messages.
func audit(t *testing.T, src string) []symbol.Finding {
	t.Helper()
	tree, err := parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	v := analyzer.NewVisitor([]byte(src), nil)
	v.Walk(tree.RootNode())
	return v.Findings()
}

func fix(t *testing.T, src string, level Level) (string, Result) {
	t.Helper()
	res, err := Apply([]byte(src), audit(t, src), Options{Level: level, HasResetInterface: true})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return string(res.Content), res
}

func TestIncompleteResetAddsDefault(t *testing.T) {
	src := `<?php
namespace App;

use Symfony\Contracts\Service\ResetInterface;

class Counter implements ResetInterface
{
    private int $count = 0;
    private array $items = ['a'];

    public function run(): void
    {
        $this->count++;
        $this->items[] = 'b';
    }

    public function reset(): void
    {
    }
}
`
	out, res := fix(t, src, LevelSafe)
	if len(res.Applied) != 2 {
		t.Fatalf("expected 2 fixes, got %+v (skipped %+v)", res.Applied, res.Skipped)
	}
	for _, want := range []string{"        $this->count = 0;\n", "        $this->items = ['a'];\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if remaining := audit(t, out); len(remaining) != 0 {
		t.Errorf("findings remain after fix: %+v", remaining)
	}
}

func TestIncompleteResetExpandsEmptyInlineBody(t *testing.T) {
	src := `<?php
class C implements ResetInterface
{
    private int $n = 1;
    public function run(): void { $this->n++; }
    public function reset(): void {}
}
`
	out, _ := fix(t, src, LevelSafe)
	want := "public function reset(): void {\n        $this->n = 1;\n    }"
	if !strings.Contains(out, want) {
		t.Errorf("missing %q in:\n%s", want, out)
	}
}

func TestIncompleteResetRefusesUnsafeProperties(t *testing.T) {
	cases := map[string]string{
		"injected dependency": `<?php
class C implements ResetInterface {
    public function __construct(private Client $client) {}
    public function run(): void { $this->client->setToken('x'); }
    public function reset(): void {}
}`,
		"typed without default": `<?php
class C implements ResetInterface {
    private int $n;
    public function run(): void { $this->n = 1; }
    public function reset(): void {}
}`,
		"initialized in constructor": `<?php
class C implements ResetInterface {
    private array $n = [];
    public function __construct() { $this->n = ['boot']; }
    public function run(): void { $this->n[] = 1; }
    public function reset(): void {}
}`,
		"written by setter": `<?php
class C implements ResetInterface {
    private ?Logger $logger = null;
    public function setLogger(Logger $l): void { $this->logger = $l; }
    public function reset(): void {}
}`,
		"early return in reset": `<?php
class C implements ResetInterface {
    private int $n = 0;
    public function run(): void { $this->n++; }
    public function reset(): void { return; }
}`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			out, res := fix(t, src, LevelSafe)
			if out != src || len(res.Applied) != 0 {
				t.Errorf("expected no change, applied %+v:\n%s", res.Applied, out)
			}
		})
	}
}

func TestSafeLevelLeavesRiskyFindings(t *testing.T) {
	src := `<?php
use Symfony\Component\HttpFoundation\Request;
class C {
    private int $n = 0;
    public function run(Request $request): void {
        $this->n++;
        echo $_GET['id'];
        gc_disable();
    }
}
`
	out, res := fix(t, src, LevelSafe)
	if out != src || len(res.Applied) != 0 {
		t.Errorf("safe level changed the file:\n%s", out)
	}
}

func TestStateMutationGeneratesReset(t *testing.T) {
	src := `<?php
namespace App;

use Foo\Bar;

class Counter
{
    private int $count = 0;

    public function run(): void
    {
        $this->count++;
    }
}
`
	out, res := fix(t, src, LevelRisky)
	if len(res.Applied) != 1 {
		t.Fatalf("expected 1 fix, got %+v (skipped %+v)", res.Applied, res.Skipped)
	}
	for _, want := range []string{
		"use Foo\\Bar;\nuse Symfony\\Contracts\\Service\\ResetInterface;\n",
		"class Counter implements ResetInterface\n",
		"\n    public function reset(): void\n    {\n        $this->count = 0;\n    }\n}\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if remaining := audit(t, out); len(remaining) != 0 {
		t.Errorf("findings remain after fix: %+v", remaining)
	}
}

func TestStateMutationIsAllOrNothing(t *testing.T) {
	cases := map[string]string{
		"one property cannot be restored": `<?php
class C {
    private int $ok = 0;
    private int $noDefault;
    public function run(): void { $this->ok++; $this->noDefault = 1; }
}
`,
		"ignored mutation must persist": `<?php
class C {
    private int $ok = 0;
    private array $cache = [];
    public function run(): void {
        $this->ok++;
        $this->cache[] = 1; // @igor-ignore
    }
}
`,
		"parent class": `<?php
class C extends Base {
    private int $n = 0;
    public function run(): void { $this->n++; }
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			out, res := fix(t, src, LevelRisky)
			if out != src || len(res.Applied) != 0 {
				t.Errorf("expected no change, applied %+v:\n%s", res.Applied, out)
			}
		})
	}
}

func TestStateMutationRequiresInstalledInterface(t *testing.T) {
	src := `<?php
class C {
    private int $n = 0;
    public function run(): void { $this->n++; }
}
`
	res, err := Apply([]byte(src), audit(t, src), Options{Level: LevelRisky})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Content) != src || len(res.Skipped) != 1 {
		t.Errorf("expected a skip and no change, got %+v", res)
	}
}

func TestSuperglobalReads(t *testing.T) {
	src := `<?php
namespace App;

use Symfony\Component\HttpFoundation\Request as Req;

class C {
    public function run(Req $req): void {
        $a = $_GET['a'] ?? 'x';
        $b = trim($_POST['b']);
        $c = isset($_COOKIE['c']);
        $d = $_SERVER['HTTP_HOST'];
    }
}
`
	out, res := fix(t, src, LevelRisky)
	if len(res.Applied) != 4 {
		t.Fatalf("expected 4 fixes, got %+v (skipped %+v)", res.Applied, res.Skipped)
	}
	for _, want := range []string{
		"$req->query->all()['a'] ?? 'x'",
		"trim($req->request->all()['b'])",
		"isset($req->cookies->all()['c'])",
		"$req->server->all()['HTTP_HOST']",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestSuperglobalRefusals(t *testing.T) {
	header := "<?php\nuse Symfony\\Component\\HttpFoundation\\Request;\nclass C {\n"
	cases := map[string]string{
		"write":             `public function r(Request $request) { $_GET['a'] = 1; }`,
		"nested write":      `public function r(Request $request) { $_GET['a']['b'] = 1; }`,
		"unset":             `public function r(Request $request) { unset($_GET['a']); }`,
		"reference":         `public function r(Request $request) { $x = &$_GET['a']; }`,
		"unknown call":      `public function r(Request $request) { sort($_GET['a']); }`,
		"whole array":       `public function r(Request $request) { $x = $_GET; }`,
		"no request":        `public function r() { $x = $_GET['a']; }`,
		"unrelated Request": `public function r(\Other\Request $request) { $x = $_GET['a']; }`,
		"closure scope":     `public function r(Request $request) { $f = function () { return $_GET['a']; }; }`,
		"session":           `public function r(Request $request) { $x = $_SESSION['a']; }`,
	}
	for name, method := range cases {
		t.Run(name, func(t *testing.T) {
			src := header + method + "\n}\n"
			out, res := fix(t, src, LevelRisky)
			if out != src || len(res.Applied) != 0 {
				t.Errorf("expected no change, applied %+v:\n%s", res.Applied, out)
			}
		})
	}
}

func TestGCDisable(t *testing.T) {
	src := "<?php\nclass C {\n    public function r() {\n        gc_disable();\n        echo 1;\n    }\n}\n"
	out, _ := fix(t, src, LevelRisky)
	want := "<?php\nclass C {\n    public function r() {\n        echo 1;\n    }\n}\n"
	if out != want {
		t.Errorf("got:\n%s", out)
	}

	for name, body := range map[string]string{
		"paired with gc_enable": "gc_disable(); work(); gc_enable();",
		"braceless if":          "if ($x) gc_disable();",
	} {
		t.Run(name, func(t *testing.T) {
			src := "<?php\nclass C {\n    public function r($x) {\n        " + body + "\n    }\n}\n"
			out, res := fix(t, src, LevelRisky)
			if out != src || len(res.Applied) != 0 {
				t.Errorf("expected no change, applied %+v:\n%s", res.Applied, out)
			}
		})
	}
}

func TestIgnoredFindingsAndBrokenFilesAreUntouched(t *testing.T) {
	src := `<?php
class C implements ResetInterface {
    private int $n = 0;
    public function run(): void { $this->n++; }
    public function reset(): void {}
}
`
	findings := audit(t, src)
	for i := range findings {
		findings[i].Ignored = true
	}
	res, err := Apply([]byte(src), findings, Options{Level: LevelRisky, HasResetInterface: true})
	if err != nil || string(res.Content) != src {
		t.Errorf("ignored finding was fixed: %v\n%s", err, res.Content)
	}

	broken := "<?php\nclass C { public function reset( {\n"
	res, err = Apply([]byte(broken), []symbol.Finding{{Message: "Property 'n' of C is mutated but not reset in reset().", ContextClass: "C", Line: 2}}, Options{Level: LevelSafe})
	if err != nil || string(res.Content) != broken {
		t.Errorf("broken file was modified: %v\n%s", err, res.Content)
	}
}

func TestFixIsIdempotent(t *testing.T) {
	src := `<?php
namespace App;

class Counter
{
    private int $count = 0;

    public function run(): void
    {
        $this->count++;
    }
}
`
	once, _ := fix(t, src, LevelRisky)
	twice, res := fix(t, once, LevelRisky)
	if once != twice || len(res.Applied) != 0 {
		t.Errorf("second run changed the file:\n%s", twice)
	}
}
