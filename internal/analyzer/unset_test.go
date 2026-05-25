package analyzer

import (
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

func TestPHPVisitor_UnsetInReset(t *testing.T) {
	code := `<?php
class UnsetService implements Symfony\Contracts\Service\ResetInterface {
    private $customer;
    public function doSomething($val) {
        $this->customer = $val;
    }
    public function reset(): void {
        unset($this->customer);
    }
}`
	content := []byte(code)

	p := sitter.NewParser()
	lang := sitter.NewLanguage(php.LanguagePHP())
	_ = p.SetLanguage(lang)
	tree := p.Parse(content, nil)
	defer tree.Close()

	engine := &mockEngine{}
	v := NewVisitor(content, engine)
	v.Walk(tree.RootNode())

	findings := v.Findings()
	for _, f := range findings {
		if f.Severity == "WARNING" && (f.Message == "Property 'customer' of UnsetService is mutated but not reset in reset().") {
			t.Errorf("Property 'customer' should be considered reset by unset(), but finding was generated: %v", f.Message)
		}
	}
}
