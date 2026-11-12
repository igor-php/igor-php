package analyzer

import (
	"reflect"
	"sort"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// stateMessages runs the visitor on a class and returns its state mutation and incomplete reset
// messages, sorted.
func stateMessages(t *testing.T, code string) []string {
	t.Helper()
	content := []byte(code)
	p := sitter.NewParser()
	_ = p.SetLanguage(sitter.NewLanguage(php.LanguagePHP()))
	tree := p.Parse(content, nil)
	defer tree.Close()
	v := NewVisitor(content, &mockEngine{})
	v.Walk(tree.RootNode())
	out := []string{}
	for _, f := range v.Findings() {
		if f.Rule == "StateMutation" || f.Rule == "IncompleteReset" {
			out = append(out, f.Message)
		}
	}
	sort.Strings(out)
	return out
}

func loader(body string) string {
	return `<?php
namespace App;
final class Loader {
    private $a; private $b; private $k; private array $c = []; private array $items = [];
    private static $s;
    public function load(array $data, $key) {
        ` + body + `
    }
}`
}

func TestStateMutation_DestructuringAndForeachTargets(t *testing.T) {
	a := "Mutation of state 'a' in Loader::load()"
	b := "Mutation of state 'b' in Loader::load()"
	cases := []struct {
		name, body string
		want       []string
	}{
		{"plain writes", `$this->a = $data[0]; $this->b = $data[1];`, []string{a, b}},
		{"short destructuring", `[$this->a, $this->b] = $data;`, []string{a, b}},
		{"list()", `list($this->a, $this->b) = $data;`, []string{a, b}},
		{"skipped element", `[$this->a, , $this->b] = $data;`, []string{a, b}},
		{"keyed", `['a' => $this->a, 'b' => $this->b] = $data;`, []string{a, b}},
		{"keyed by a variable", `[$key => $this->a] = $data;`, []string{a}},
		{"nested", `[[$this->a], $this->b] = $data;`, []string{a, b}},
		{"by reference", `[&$this->a] = $data;`, []string{a}},
		{"appended", `[$x, $this->c[]] = $data;`, []string{"Mutation of state 'c' in Loader::load()"}},
		{"static property", `[self::$s] = $data;`, []string{"Mutation of state 'static::$s' in Loader::load()"}},
		{"foreach value", `foreach ($data as $this->a) {}`, []string{a}},
		{"foreach key and value", `foreach ($data as $this->a => $this->b) {}`, []string{a, b}},
		{"foreach destructuring", `foreach ($data as [$this->a, $this->b]) {}`, []string{a, b}},
		{"foreach by reference", `foreach ($data as &$this->a) {}`, []string{a}},
		{"foreach alternative syntax", `foreach ($data as $k => list('x' => $this->a)): endforeach;`, []string{a}},

		// Reads are not writes
		{"property used as a key", `[$this->k => $x] = $data;`, []string{}},
		{"property iterated", `foreach ($this->items as $item) { $item->touch(); }`, []string{}},
		{"locals only", `[$x, $y] = $data; foreach ($data as $k => $v) {}`, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stateMessages(t, loader(c.body)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestStateMutation_DestructuringExemptions(t *testing.T) {
	constructor := `<?php
final class Loader {
    private $a; private $b;
    public function __construct(array $data) { [$this->a, $this->b] = $data; foreach ($data as $this->a) {} }
}`
	if got := stateMessages(t, constructor); len(got) != 0 {
		t.Errorf("a constructor sets the initial state: got %q", got)
	}

	cleared := `<?php
final class Loader {
    private $a; private $b;
    public function load(array $data) { $this->a = $data; }
    public function clear() { [$this->a, $this->b] = [null, []]; }
}`
	if got := stateMessages(t, cleared); !reflect.DeepEqual(got, []string{"Mutation of state 'a' in Loader::load()"}) {
		t.Errorf("clear() emptying properties through a destructuring is the reset itself: got %q", got)
	}

	notEmpty := `<?php
final class Loader {
    private $a; private $b;
    public function clear(array $data) { [$this->a, $this->b] = [$data, null]; }
}`
	if got := stateMessages(t, notEmpty); !reflect.DeepEqual(got, []string{"Mutation of state 'a' in Loader::clear()", "Mutation of state 'b' in Loader::clear()"}) {
		t.Errorf("clear() writing a value is a mutation: got %q", got)
	}

	resettable := `<?php
class Loader implements Symfony\Contracts\Service\ResetInterface {
    private $a; private $b; private $c;
    public function load(array $data) { [$this->a, $this->b, $this->c] = $data; }
    public function reset(): void { [$this->a, $this->b] = [null, null]; }
}`
	if got := stateMessages(t, resettable); !reflect.DeepEqual(got, []string{"Property 'c' of Loader is mutated but not reset in reset()."}) {
		t.Errorf("reset() clearing through a destructuring counts, the property it leaves does not: got %q", got)
	}
}

// A method writing its state through foreach is not stateless: the auditor must not drop a
// dependency mutation finding on it.
func TestMethodState_ForeachTargetWritesState(t *testing.T) {
	code := []byte(`<?php
namespace App;
class Cursor {
    private $current;
    public function setRows(array $rows) { foreach ($rows as $this->current) {} }
    public function setPairs(array $rows) { foreach ($rows as $k => [$this->current]) {} }
    public function setLocal(array $rows) { foreach ($rows as $row) { $row->touch(); } }
}`)
	engine := &stateEngine{}
	p := sitter.NewParser()
	_ = p.SetLanguage(sitter.NewLanguage(php.LanguagePHP()))
	tree := p.Parse(code, nil)
	defer tree.Close()
	NewVisitor(code, engine).Walk(tree.RootNode())
	for method, want := range map[string]bool{"setRows": true, "setPairs": true, "setLocal": false} {
		if got := engine.states[method].Mutates; got != want {
			t.Errorf("%s: Mutates = %v, want %v", method, got, want)
		}
	}
}

type stateEngine struct {
	mockEngine
	states map[string]MethodState
}

func (e *stateEngine) RecordClassShape(string, ClassShape) {}

func (e *stateEngine) RecordMethodState(_ string, method string, state MethodState) {
	if e.states == nil {
		e.states = map[string]MethodState{}
	}
	e.states[method] = state
}
