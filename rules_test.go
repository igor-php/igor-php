package igorphp

import (
	"strings"
	"testing"

	"github.com/igor-php/igor-php/pkg/symbol"
)

func TestEveryRuleHasADoc(t *testing.T) {
	for _, id := range symbol.Rules {
		doc, ok := RuleDoc(id)
		if !ok || !strings.Contains(doc, "**Fix**") {
			t.Errorf("rule %s has no section with a **Fix** in the rule reference", id)
		}
	}
	if len(RuleIDs()) != len(symbol.Rules) {
		t.Errorf("the rule reference lists %v, symbol.Rules %v", RuleIDs(), symbol.Rules)
	}
}

func TestRuleDocIsCaseInsensitive(t *testing.T) {
	if _, ok := RuleDoc("statemutation"); !ok {
		t.Fatal("RuleDoc must ignore the case of the identifier")
	}
	if _, ok := RuleDoc("NoSuchRule"); ok {
		t.Fatal("RuleDoc must report an unknown rule")
	}
}
