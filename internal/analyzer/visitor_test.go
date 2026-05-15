package analyzer

import (
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// mockEngine implements the Engine interface for testing.
type mockEngine struct {
	auditedClasses []string
}

func (m *mockEngine) RecordClassAudited(name string) {
	m.auditedClasses = append(m.auditedClasses, name)
}

func (m *mockEngine) IsExplicitlyNonShared(className string) bool {
	return false
}

func (m *mockEngine) IsSafeNamespace(className string) bool {
	return false
}

func TestPHPVisitor_Mutation(t *testing.T) {
	code := `<?php
class MyService {
    private $prop;
    public function set($v) {
        $this->prop = $v;
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
	if len(findings) == 0 {
		t.Fatal("Expected at least one finding for state mutation, got 0")
	}

	found := false
	for _, f := range findings {
		if f.Severity == "ERROR" && f.Message != "" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected an ERROR finding for state mutation")
	}

	if len(engine.auditedClasses) == 0 || engine.auditedClasses[0] != "MyService" {
		t.Errorf("Expected class 'MyService' to be recorded as audited, got %v", engine.auditedClasses)
	}
}

func TestPHPVisitor_ResetInterface(t *testing.T) {
	code := `<?php
class MyService implements Symfony\Contracts\Service\ResetInterface {
    private $prop;
    public function set($v) {
        $this->prop = $v;
    }
    public function reset() {
        // Not resetting $prop
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
	found := false
	for _, f := range findings {
		if f.Severity == "WARNING" && (f.Message == "Property 'prop' of MyService is mutated but not reset in reset().") {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected a WARNING finding for missing reset in ResetInterface")
	}
}
