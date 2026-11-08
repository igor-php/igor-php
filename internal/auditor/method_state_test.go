package auditor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

func TestDropStatelessDependencyCalls(t *testing.T) {
	aud := NewAuditor(config.Config{})
	path := filepath.Join("..", "..", "test", "fixtures", "stateless_dependency_calls.php")

	findings, err := aud.Audit(path, nil)
	if err != nil {
		t.Fatalf("failed to audit fixture: %v", err)
	}
	before := dependencyMutations(findings)
	for _, m := range []string{"rate", "renameFreshIndex"} {
		if !before[m] {
			t.Fatalf("%s: expected the name-based finding before the drop", m)
		}
	}
	for _, f := range findings {
		if strings.Contains(f.Message, "($statistics)") {
			t.Errorf("a collection built by collect() is not a shared service: %s", f.Message)
		}
	}

	results := []symbol.AuditStatus{{FilePath: path, Findings: findings}}
	if !aud.DropStatelessDependencyCalls(results) {
		t.Fatal("expected some dependency mutation findings to be dropped")
	}

	reported := dependencyMutations(results[0].Findings)

	cases := []struct {
		method string
		kept   bool
		why    string
	}{
		{"rate", false, "RatingService::setRating() only writes to the database"},
		{"rateThroughInterface", true, "an interface says nothing of the injected class"},
		{"switchLocale", true, "setLocale() assigns $this->locale"},
		{"addLocales", true, "addLocales() assigns through a private method"},
		{"addContext", true, "addContext() inserts into a queue the auditor cannot read"},
		{"register", true, "registerHandler() writes a static property"},
		{"notifyAbstract", true, "an abstract class may be any subclass"},
		{"notifyParent", true, "MailNotifier::setRecipient() writes through parent::"},
		{"flash", true, "addMessage() comes from a trait"},
		{"renameSharedIndex", true, "setRootName() renames the shared index it is given"},
		{"renameFreshIndex", false, "setRootName() only renames a new index"},
		{"callUnknown", true, "an unaudited class keeps the name-based guess"},
		{"writeThroughAlias", true, "setValue() writes $this->value through local aliases of $this"},
		{"renameNestedSharedIndex", true, "setRoot() hands its own index to a method that renames it"},
		{"changeTimezone", true, "setTimezone() changes the process timezone"},
		{"setDynamicOption", true, "setOption() calls any method on its own options"},
		{"setOverriddenValue", true, "a subclass overrides the empty setValue() with a write"},
		{"clearStaticCache", true, "clearCache() flushes a static registry it cannot read"},
		{"addSongsForSharedUser", false, "addSongs() changes the playlist it is given, not the shared user"},
		{"addSongsToSharedPlaylist", true, "addSongs() changes the shared playlist it is given"},
	}
	for _, c := range cases {
		if reported[c.method] != c.kept {
			t.Errorf("%s: reported=%v, want %v (%s)", c.method, reported[c.method], c.kept, c.why)
		}
	}
}

// dependencyMutations lists the controller methods with a dependency mutation finding.
func dependencyMutations(findings []symbol.Finding) map[string]bool {
	reported := map[string]bool{}
	for _, f := range findings {
		if strings.Contains(f.Message, "injected dependency") && strings.HasSuffix(f.ContextClass, "StatelessCallsController") {
			reported[f.ContextMethod] = true
		}
	}
	return reported
}
