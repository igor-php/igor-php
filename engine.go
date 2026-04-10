package main

import (
	"fmt"
	"io/ioutil"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

func analyzeFile(path string) ([]Finding, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	p := sitter.NewParser()
	lang := sitter.NewLanguage(php.LanguagePHP())
	_ = p.SetLanguage(lang)

	tree := p.Parse(content, nil)
	if tree == nil {
		return nil, fmt.Errorf("error parsing %s", path)
	}
	defer tree.Close()

	v := &FrankenVisitor{
		content:  content,
		lines:    strings.Split(string(content), "\n"),
		mutated:  make(map[string]mutationInfo),
		resetted: make(map[string]bool),
	}
	v.walk(tree.RootNode())

	return v.findings, nil
}
