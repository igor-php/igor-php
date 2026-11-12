package analyzer

import (
	"reflect"
	"sort"
	"testing"

	"github.com/igor-php/igor-php/pkg/symbol"
	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// staleProperties runs the stale dependency rule under a worker model and returns the properties
// it reports, sorted.
func staleProperties(t *testing.T, persistence string) []string {
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
	v.SetPersistence(persistence)
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

func TestStaleDependency_PersistenceModel(t *testing.T) {
	request := "Property 'request' of ReportService keeps the HTTP request (Illuminate\\Http\\Request) across requests."
	eitherRequest := "Property 'either' of ReportService keeps the HTTP request (Illuminate\\Http\\Request) across requests."
	eitherApp := "Property 'either' of ReportService keeps the application container (Illuminate\\Contracts\\Foundation\\Application) across requests."
	octane := []string{
		"Property 'app' of ReportService keeps the application container (Illuminate\\Contracts\\Foundation\\Application) across requests.",
		"Property 'config' of ReportService keeps the configuration repository (Illuminate\\Contracts\\Config\\Repository) across requests.",
		"Property 'container' of ReportService keeps the application container (Illuminate\\Container\\Container) across requests.",
		eitherApp,
		"Property 'maybeApp' of ReportService keeps the application container (Illuminate\\Contracts\\Foundation\\Application) across requests.",
		request,
	}
	sort.Strings(octane)

	cases := []struct {
		persistence string
		want        []string
		why         string
	}{
		{symbol.PersistenceOctane, octane, "Octane clones the container and the config for every request"},
		{"", octane, "an unknown model (Symfony, or a map written before the field) keeps every finding"},
		{symbol.PersistenceWorker, []string{eitherRequest, request}, "without Octane, only the request belongs to one request"},
	}
	for _, c := range cases {
		if got := staleProperties(t, c.persistence); !reflect.DeepEqual(got, c.want) {
			t.Errorf("persistence %q (%s):\n got  %q\n want %q", c.persistence, c.why, got, c.want)
		}
	}
}
