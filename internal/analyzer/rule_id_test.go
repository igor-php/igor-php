package analyzer

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/igor-php/igor-php/pkg/symbol"
	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// TestEveryFindingHasARuleID runs the visitor on every PHP fixture and lab file, and checks that each
// finding names one of the rules of the catalog, so tools can rely on rule_id instead of the message.
func TestEveryFindingHasARuleID(t *testing.T) {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(php.LanguagePHP())); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	files := 0
	for _, root := range []string{"../../test/fixtures", "../../examples"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "vendor" || d.Name() == "var" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".php") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files++
			for _, framework := range []string{"", FrameworkLaravel} {
				tree := parser.Parse(content, nil)
				v := NewVisitor(content, &mockEngine{})
				v.framework = framework
				v.Walk(tree.RootNode())
				tree.Close()
				for _, f := range v.Findings() {
					if !slices.Contains(symbol.Rules, f.Rule) {
						t.Errorf("%s:%d: finding %q has rule_id %q, not one of symbol.Rules", path, f.Line, f.Message, f.Rule)
					}
					seen[f.Rule] = true
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	t.Logf("%d files, rules reported: %v", files, seen)
	if files == 0 || len(seen) < 8 {
		t.Fatalf("expected the fixtures to cover most rules, got %d files and rules %v", files, seen)
	}
}
