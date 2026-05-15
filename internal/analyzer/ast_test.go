package analyzer

import (
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

func TestNodeASTDetails(t *testing.T) {
	code := `<?php $this->prop = 1;`
	content := []byte(code)

	p := sitter.NewParser()
	lang := sitter.NewLanguage(php.LanguagePHP())
	_ = p.SetLanguage(lang)
	tree := p.Parse(content, nil)
	defer tree.Close()

	// Find the assignment node
	// The structure is usually (program (expression_statement (assignment_expression ...)))
	root := tree.RootNode()
	exprStmt := root.NamedChild(1)
	if exprStmt == nil {
		t.Fatalf("Expected second named child, got nil")
	}
	t.Logf("Second named child kind: %s", exprStmt.Kind())
	if exprStmt.Kind() != "expression_statement" {
		t.Fatalf("Expected expression_statement, got %s", exprStmt.Kind())
	}
	assignExpr := exprStmt.NamedChild(0)
	if assignExpr == nil || assignExpr.Kind() != "assignment_expression" {
		t.Fatalf("Expected assignment_expression, got %v", assignExpr)
	}

	astDetails := assignExpr.ToSexp()
	if astDetails == "" {
		t.Fatal("Expected non-empty AST details")
	}

	if assignExpr.Kind() != "assignment_expression" {
		t.Errorf("Expected assignment_expression, got %s", assignExpr.Kind())
	}
}
