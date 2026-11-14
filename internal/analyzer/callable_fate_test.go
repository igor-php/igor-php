package analyzer

import (
	"reflect"
	"testing"
)

func fateOf(t *testing.T, body string, param int) ParamFate {
	t.Helper()
	code := "<?php\nnamespace App;\nuse Vendor\\Registry;\nclass Subject extends Base {\n" +
		"    private Registry $registry;\n    private $untyped;\n    private array $listeners = [];\n    private static array $all = [];\n" +
		"    public function run(callable $callback, array $options = [], callable ...$rest) {\n" + body + "\n    }\n}\n"
	classes := SummarizeCallables([]byte(code))
	class, ok := classes["App\\Subject"]
	if !ok {
		t.Fatalf("class not summarized: %v", classes)
	}
	method, ok := class.Methods["run"]
	if !ok || !method.HasBody || len(method.Params) != 3 {
		t.Fatalf("method not summarized: %+v", method)
	}
	return method.Params[param]
}

func TestParamFate(t *testing.T) {
	invoked := ParamFate{}
	wrap := ParamForward{Kind: ForwardSelf, Method: "wrap", Pos: 0}
	decorate := ParamForward{Kind: ForwardDependency, Class: "Vendor\\Registry", Method: "decorate", Pos: 0}
	replace := ParamForward{Kind: ForwardSelf, Method: "replace", Pos: 0}
	retained := ParamFate{Retained: true}
	global := ParamFate{Retained: true, Global: true}
	unknown := ParamFate{Unknown: true}

	cases := []struct {
		name string
		body string
		want ParamFate
	}{
		{"called", `return $callback($this);`, invoked},
		{"called in a try", `try { $r = $callback(); } finally { $this->close(); } return $r;`, invoked},
		{"unused", `return 1;`, invoked},
		{"call_user_func", `return \call_user_func($callback, 1);`, invoked},
		{"array_map", `return array_map($callback, $options);`, ParamFate{Trusts: []string{"App\\array_map"}}},
		{"tested and returned", `if (null === $callback || !is_callable($callback)) { return null; } return $callback;`, ParamFate{Returned: true}},
		{"local alias called", `$fn = $callback; return $fn();`, invoked},
		{"closure around it, called", `$wrapped = function () use ($callback) { return $callback(); }; return $wrapped();`, invoked},
		{"arrow function around it, called", `$wrapped = fn () => $callback(); return $wrapped();`, invoked},
		{"local array, iterated", `$all = [$callback]; foreach ($all as $one) { $one(); }`, invoked},
		{"local array filled then iterated", `$all = []; $all['first'] = $callback; foreach ($all as $one) { $one(); }`, invoked},
		{"local object given it", `$box = new Box(); $box->callback = $callback; return $box;`, ParamFate{Returned: true}},
		{"method called on it", `$callback->handle($options);`, invoked},
		{"local alias cleared before it is stored", `$fn = $callback; $fn = null; $this->untyped = $fn;`, invoked},
		{"local replaced by an unrelated value before it is stored", `$fn = $callback; $fn = $options['fallback']; $this->untyped = $fn;`, invoked},
		{"parameter overwritten before it is stored", `$callback(); $callback = null; $this->untyped = $callback;`, invoked},

		{"stored in a property", `$this->untyped = $callback;`, retained},
		{"appended to a property", `$this->listeners[$options['event']][] = $callback;`, retained},
		{"stored in a static property", `self::$all[] = $callback;`, global},
		{"stored through a local alias", `$fn = $callback; $this->listeners[] = $fn;`, retained},
		{"stored inside an array", `$this->listeners[] = [$callback, 10];`, retained},
		{"stored inside a closure", `$this->listeners[] = function () use ($callback) { return $callback(); };`, retained},
		{"stored inside an arrow function", `$this->listeners[] = fn () => $callback();`, retained},
		{"stored inside a new object", `$this->listeners[] = new Wrapper($callback);`,
			ParamFate{Retained: true, Forwards: []ParamForward{{Kind: ForwardStatic, Class: "App\\Wrapper", Method: "__construct", Pos: 0}}}},
		{"stored with ??=", `$this->untyped ??= $callback;`, retained},
		{"stored with +=", `$this->listeners += [$callback];`, retained},
		{"stored through another parameter", `$options = [$callback]; $this->listeners = $options;`, retained},
		{"appended to an array parameter then stored", `$options[] = $callback; $this->listeners = $options;`, retained},
		{"stored as a first-class callable", `$this->untyped = $callback(...);`, retained},
		{"stored after Closure::fromCallable", `$this->untyped = \Closure::fromCallable($callback);`, retained},
		{"stored after bindTo", `$this->untyped = $callback->bindTo($this);`, retained},
		{"stored with a default", `$this->untyped = $callback ?? 'strlen';`, retained},
		{"array_push on a property", `array_push($this->listeners, $callback);`, retained},
		{"local array then stored", `$all = []; $all[] = $callback; $this->listeners = $all;`, retained},
		{"stored later in a loop", `foreach ($options as $o) { $this->listeners[] = $fn; $fn = $callback; }`, retained},
		{"appended to a copy of a property, then stored back", `$all = $this->listeners; $all[] = $callback; $this->listeners = $all;`,
			ParamFate{Retained: true, Unknown: true}},
		{"copy of a property cleared before it is stored back", `$all = $this->listeners; $all[] = $callback; $all = []; $this->listeners = $all;`, unknown},
		{"parameter replaced by a closure around it", `$callback = \Closure::fromCallable($callback); $this->untyped = $callback;`, retained},
		{"parameter replaced by a wrapper of it", `$callback = $this->wrap($callback); $this->untyped = $callback;`,
			ParamFate{Retained: true, Forwards: []ParamForward{wrap}, ResultKept: &ParamFate{Retained: true, Forwards: []ParamForward{wrap}}, ResultFrom: []ParamForward{wrap}}},
		{"result of a method given it, stored", `$wrapped = $this->wrap($callback); $this->untyped = $wrapped;`,
			ParamFate{Forwards: []ParamForward{wrap}, ResultKept: &ParamFate{Retained: true}, ResultFrom: []ParamForward{wrap}}},
		{"result of a method given it, replacing a local, stored", `$fn = $callback; $wrapped = $this->wrap($fn); $fn = $wrapped; $this->untyped = $fn;`,
			ParamFate{Forwards: []ParamForward{wrap}, ResultKept: &ParamFate{Retained: true, Forwards: []ParamForward{wrap}}, ResultFrom: []ParamForward{wrap}}},
		{"result of a method given the result of another, stored", `$this->untyped = $this->replace($this->wrap($callback));`,
			ParamFate{Forwards: []ParamForward{wrap}, ResultKept: &ParamFate{Forwards: []ParamForward{replace}, ResultKept: &ParamFate{Retained: true}, ResultFrom: []ParamForward{replace}}, ResultFrom: []ParamForward{wrap}}},
		{"result of a dependency given it, stored", `$this->listeners[] = $this->registry->decorate($callback);`,
			ParamFate{Forwards: []ParamForward{decorate}, ResultKept: &ParamFate{Retained: true}, ResultFrom: []ParamForward{decorate}}},
		{"generator", `yield 1; $callback();`, ParamFate{Returned: true}},
		{"generator in a closure only", `$gen = function () { yield 1; }; $callback();`, invoked},
		{"result of a method given it, returned", `$result = $this->wrap($callback); return $result;`,
			ParamFate{Returned: true, Forwards: []ParamForward{wrap}}},
		{"local replaced by a value built from another variable", `$fn = $callback; $fn = $options['wrap']($fn); $this->untyped = $fn;`,
			ParamFate{Retained: true, Unknown: true}},
		{"local replaced by another holder of it", `$all = [$callback]; $fn = null; $fn = $all[0]; $this->untyped = $fn;`, retained},
		{"cleared in a branch only", `$fn = $callback; if ($options) { $fn = null; } $this->untyped = $fn;`, retained},
		{"cleared then given again", `$fn = $callback; $fn = null; $fn = $callback; $this->untyped = $fn;`, retained},
		{"rebuilt from itself", `$all = [$callback]; $all = array_merge($all, $options); $this->listeners = $all;`, retained},
		{"cleared after it is stored", `$fn = $callback; $this->untyped = $fn; $fn = null;`, retained},
		{"shutdown function", `register_shutdown_function($callback);`, global},
		{"static variable", `static $kept = $callback;`, global},
		{"superglobal", `$GLOBALS['cb'] = $callback;`, global},

		{"unknown function", `tap($callback);`, unknown},
		{"namespaced function named like a PHP one", `\App\array_map($callback, $options);`, unknown},
		{"new object left to its constructor", `new Wrapper($callback);`,
			ParamFate{Forwards: []ParamForward{{Kind: ForwardStatic, Class: "App\\Wrapper", Method: "__construct", Pos: 0}}}},
		{"new anonymous class", `new class($callback) {};`, unknown},
		{"method on a local", `$helper = new Helper(); $helper->keep($callback);`, unknown},
		{"method on an untyped property", `$this->untyped->keep($callback);`, unknown},
		{"named argument", `$this->keep(listener: $callback);`, unknown},
		{"func_get_args", `$this->listeners = func_get_args();`, unknown},
		{"reference", `$ref = &$callback;`, unknown},
		{"stored in an object of the caller", `$options['holder']->callback = $callback;`, unknown},
		{"stored in a local taken from the object", `$all = $this->listeners; $all->callbacks[] = $callback;`, unknown},
		{"stored in a local taken from a dependency", `$bag = $this->registry->bag(); $bag['callback'] = $callback;`, unknown},
		{"stored in a loop variable", `foreach ($this->listeners as $holder) { $holder->callback = $callback; }`, unknown},
		{"stored in a caught object", `try { run(); } catch (\Throwable $e) { $e->callback = $callback; }`, unknown},

		{"passed to a method of the class", `$this->keep('x', $callback);`,
			ParamFate{Forwards: []ParamForward{{Kind: ForwardSelf, Method: "keep", Pos: 1}}}},
		{"passed to self::", `self::keep($callback);`,
			ParamFate{Forwards: []ParamForward{{Kind: ForwardSelf, Method: "keep", Pos: 0, Self: true}}}},
		{"passed to parent::", `parent::run($callback);`,
			ParamFate{Forwards: []ParamForward{{Kind: ForwardParent, Method: "run", Pos: 0}}}},
		{"passed to a dependency", `$this->registry->add('x', $callback);`,
			ParamFate{Forwards: []ParamForward{{Kind: ForwardDependency, Class: "Vendor\\Registry", Method: "add", Pos: 1}}}},
		{"passed to a static method", `Registry::compute($callback, $options);`,
			ParamFate{Forwards: []ParamForward{{Kind: ForwardStatic, Class: "Vendor\\Registry", Method: "compute", Pos: 0}}}},
		{"wrapped then passed on", `$this->registry->add(function () use ($callback) { $callback(); });`,
			ParamFate{Forwards: []ParamForward{{Kind: ForwardDependency, Class: "Vendor\\Registry", Method: "add", Pos: 0}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := fateOf(t, c.body, 0)
			if c.want.Trusts == nil {
				// The built-in functions a body relies on have their own case
				got.Trusts = nil
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// An entry written on a parameter is a local copy only when the parameter is an array passed by
// value: an \ArrayAccess object, or an untyped parameter, may be shared.
func TestParamFateEntryOfAParameter(t *testing.T) {
	fate := func(signature, body string) ParamFate {
		t.Helper()
		code := "<?php\nclass S { private array $kept = [];\n public function run(" + signature + ") {\n" + body + "\n} }"
		return SummarizeCallables([]byte(code))["S"].Methods["run"].Params[0]
	}
	write := `$options['callback'] = $callback;`
	for _, signature := range []string{
		`callable $callback, \ArrayAccess $options`,
		`callable $callback, $options`,
		`callable $callback, array &$options`,
		`callable $callback, iterable $options`,
	} {
		if got := fate(signature, write); !got.Unknown || got.Retained {
			t.Errorf("%s: an entry of this parameter may be shared, got %+v", signature, got)
		}
	}
	for _, signature := range []string{
		`callable $callback, array $options = []`,
		`callable $callback, ?array $options = null`,
		`callable $callback, callable ...$options`,
	} {
		if got := fate(signature, write); got.Unknown || got.Retained {
			t.Errorf("%s: an entry of this parameter is a local copy, got %+v", signature, got)
		}
		if got := fate(signature, write+` $this->kept = $options;`); !got.Retained {
			t.Errorf("%s: the copy is then stored, got %+v", signature, got)
		}
	}
}

func TestParamFateVariadicAndOthers(t *testing.T) {
	if got := fateOf(t, `foreach ($rest as $one) { $this->listeners[] = $one; }`, 2); !got.Retained {
		t.Errorf("variadic stored through foreach: got %+v", got)
	}
	if got := fateOf(t, `foreach ($rest as $one) { $one(); }`, 2); got.Retained || got.Unknown || len(got.Forwards) > 0 {
		t.Errorf("variadic called through foreach: got %+v", got)
	}
	// What happens to one parameter says nothing about the others
	if got := fateOf(t, `$this->listeners[] = $callback; return $options;`, 1); got.Retained || got.Unknown {
		t.Errorf("other parameter: got %+v", got)
	}
}

func TestSummarizeCallablesShapes(t *testing.T) {
	code := `<?php
namespace App;
use Lib\Tools;
abstract class Base implements Contract { use Tools, \Lib\More, Local; abstract public function run(callable $c);
    public function __construct(private \Closure $factory, callable &$ref) {} }
interface Contract { public function run(callable $c); }
trait Local { public function keep(callable $c) { $this->kept = $c; } }
final class Leaf extends Base { public function run(callable $c) { return $c(); } }
`
	classes := SummarizeCallables([]byte(code))
	base := classes["App\\Base"]
	if base.Concrete || !reflect.DeepEqual(base.Traits, []string{"Lib\\Tools", "Lib\\More", "App\\Local"}) {
		t.Errorf("Base: %+v", base)
	}
	if base.Methods["run"].HasBody {
		t.Errorf("abstract method has a body")
	}
	ctor := base.Methods["__construct"].Params
	if !ctor[0].Retained || !ctor[1].Unknown {
		t.Errorf("constructor: %+v", ctor)
	}
	if classes["App\\Contract"].Concrete || classes["App\\Local"].Concrete {
		t.Errorf("interface or trait reported concrete")
	}
	if !classes["App\\Local"].Methods["keep"].Params[0].Retained {
		t.Errorf("trait method not summarized")
	}
	if base.Methods["__construct"].Opaque {
		t.Errorf("constructor reported opaque")
	}
	variadic := SummarizeCallables([]byte(`<?php class V { function add() { $this->all = func_get_args(); } }`))
	if !variadic["V"].Methods["add"].Opaque {
		t.Errorf("func_get_args() must make the method opaque")
	}
	if !reflect.DeepEqual(base.Interfaces, []string{"App\\Contract"}) || base.TraitRules {
		t.Errorf("Base interfaces and trait rules: %+v", base)
	}
	adapted := SummarizeCallables([]byte(`<?php class A { use Immediate, Deferred { Deferred::run insteadof Immediate; } }
interface Sub extends \Top, Other {}`))
	if !adapted["A"].TraitRules || !reflect.DeepEqual(adapted["Sub"].Interfaces, []string{"Top", "Other"}) {
		t.Errorf("trait rules or interface parents: %+v", adapted)
	}
	shadow := SummarizeCallables([]byte("<?php\nnamespace App;\nuse function Lib\\array_map;\nclass S { function run(callable $c) { array_map($c, []); } }"))
	if !shadow["App\\S"].Methods["run"].Params[0].Unknown {
		t.Errorf("an imported function named like a PHP one must not be trusted")
	}
	leaf := classes["App\\Leaf"]
	if !leaf.Concrete || leaf.Parent != "App\\Base" {
		t.Errorf("Leaf: %+v", leaf)
	}
}
