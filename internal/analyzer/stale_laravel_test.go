package analyzer

import (
	"reflect"
	"sort"
	"testing"

	"github.com/igor-php/igor-php/pkg/symbol"
	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// staleProperties runs the stale dependency rule on a Laravel service and returns the properties
// it reports, sorted.
func staleProperties(t *testing.T) []string {
	t.Helper()
	content := []byte(`<?php
namespace App\Services;

use Illuminate\Contracts\Config\Repository;
use Illuminate\Contracts\Foundation\Application;
use Illuminate\Http\Request;

final class ReportService
{
    private \Illuminate\Container\Container $container;

    public function __construct(
        private Application $app,
        private Repository $config,
        private Request $request,
        private Application|Request $either,
        private ?Application $maybeApp,
    ) {}
}`)
	p := sitter.NewParser()
	_ = p.SetLanguage(sitter.NewLanguage(php.LanguagePHP()))
	tree := p.Parse(content, nil)
	defer tree.Close()

	v := NewVisitor(content, &mockEngine{})
	v.SetFramework(FrameworkLaravel)
	v.Walk(tree.RootNode())

	props := []string{}
	for _, f := range v.Findings() {
		if f.Rule == symbol.RuleStaleDependency {
			props = append(props, f.Message)
		}
	}
	sort.Strings(props)
	return props
}

// Octane serves each request from its own clone of the container and the config: a persistent
// service that keeps them, or the request, keeps the first request's copy (#121).
func TestStaleDependency_LaravelProperties(t *testing.T) {
	want := []string{
		"Property 'app' of ReportService keeps the application container (Illuminate\\Contracts\\Foundation\\Application) across requests.",
		"Property 'config' of ReportService keeps the configuration repository (Illuminate\\Contracts\\Config\\Repository) across requests.",
		"Property 'container' of ReportService keeps the application container (Illuminate\\Container\\Container) across requests.",
		"Property 'either' of ReportService keeps the application container (Illuminate\\Contracts\\Foundation\\Application) across requests.",
		"Property 'maybeApp' of ReportService keeps the application container (Illuminate\\Contracts\\Foundation\\Application) across requests.",
		"Property 'request' of ReportService keeps the HTTP request (Illuminate\\Http\\Request) across requests.",
	}
	sort.Strings(want)
	if got := staleProperties(t); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
