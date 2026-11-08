package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/igor-php/igor-php/pkg/symbol"
)

func TestRuleSubcommand(t *testing.T) {
	var out bytes.Buffer
	if err := handleRuleSubcommand([]string{"rule"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, id := range symbol.Rules {
		if !strings.Contains(out.String(), "  "+id+"\n") {
			t.Errorf("rule list misses %s:\n%s", id, out.String())
		}
	}

	out.Reset()
	if err := handleRuleSubcommand([]string{"rule", symbol.RuleUnsafeStateRestore}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "# UnsafeStateRestore\n") || !strings.Contains(out.String(), "finally") {
		t.Fatalf("unexpected rule doc:\n%s", out.String())
	}

	if err := handleRuleSubcommand([]string{"rule", "NoSuchRule"}, &out); err == nil || !strings.Contains(err.Error(), "StateMutation") {
		t.Fatalf("an unknown rule must fail and list the rules, got %v", err)
	}
}
