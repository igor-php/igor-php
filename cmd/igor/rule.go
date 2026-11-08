package main

import (
	"fmt"
	"io"
	"strings"

	igorphp "github.com/igor-php/igor-php"
)

// handleRuleSubcommand prints the reference of a rule (why it leaks in worker mode, the Symfony and
// Laravel fix, the false positives), or the list of rules without argument.
func handleRuleSubcommand(args []string, out io.Writer) error {
	if len(args) < 2 {
		_, _ = fmt.Fprintf(out, "Rules (pass one to %s rule <rule_id>):\n", binName)
		for _, id := range igorphp.RuleIDs() {
			_, _ = fmt.Fprintf(out, "  %s\n", id)
		}
		return nil
	}

	doc, ok := igorphp.RuleDoc(args[1])
	if !ok {
		return fmt.Errorf("unknown rule %q, expected one of: %s", args[1], strings.Join(igorphp.RuleIDs(), ", "))
	}
	_, err := fmt.Fprintf(out, "# %s\n\n%s\n", args[1], doc)
	return err
}
