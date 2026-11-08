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
	for _, m := range []string{"rate", "renameFreshIndex", "renameHeldSharedIndex", "readEncoding"} {
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
		{"switchTenant", true, "setTenant() writes $_ENV"},
		{"renameHeldSharedIndex", true, "setHeldName() renames the index held by the shared holder it is given"},
		{"renameSharedIndexWithHelper", true, "setNameWith() hands the shared index to a local helper's setter"},
		{"renameFreshIndexWithHelper", false, "setNameWith() only hands a new index to a local helper"},
		{"renameFromSharedFactory", false, "setFreshName() renames a new index built by the shared factory"},
		{"collectSharedIndex", false, "setItems() only stores the shared index in a local collection"},
		{"setSharedIndexWithHelper", true, "setWithHelper() hands the shared index to a helper's set(), which is not a collection"},
		{"renameStoredSharedIndex", true, "setFirstName() renames the shared index through the collection it stored it in"},
		{"renameAliasedSharedIndex", true, "setAliasName() renames the shared index through a local alias"},
		{"pushThroughReusedLocal", true, "setQueued() appends to its own queue through a local that held a collection"},
		{"touchInherited", true, "the inherited setValue() calls self::touch(), which is the parent's writing touch()"},
		{"renameWrappedSharedIndex", true, "setItems() hands the shared index, wrapped in an array, to a method that renames it"},
		{"bumpSharedCounter", true, "setCount() increments a property of the shared counter it is given"},
		{"resetSharedCounters", true, "setAll() writes each shared counter it loops over"},
		{"advanceCursor", true, "setNext() calls advance(), which moves its own cursor, through a local"},
		{"pushCurrentQueue", true, "setCurrent() pushes onto a queue current() hands back from its own state"},
		{"renameClonedChild", true, "setCopyName() renames the index a shallow clone still shares"},
		{"renameClone", false, "setCopy() only changes the clone itself"},
		{"advanceBranchCursor", true, "setNext() calls advance() on either cursor, one of which moves"},
		{"advanceReplacedCursor", false, "setQuiet() replaces the moving cursor before calling advance()"},
		{"advanceFluentCursor", true, "setFluent() calls advance() on the moving cursor before reassigning the local"},
		{"trimSharedOptions", false, "setOptions() only changes its own copy of the array"},
		{"readEncoding", false, "setEncodingCheck() reads the encoding and calls the namespace's own setlocale()"},
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
