// Package igorphp gives the Go code access to files of the repository that are not Go code, such as
// the rule reference of the Claude Code plugin, so that they have a single source.
package igorphp

import (
	_ "embed"
	"strings"
)

// rulesReference is the rule reference of the frankenphp-worker-audit skill: one "## <RuleId>"
// section per rule with its messages, why it leaks, the Symfony and Laravel fix and its false positives.
//
//go:embed plugins/igor/skills/frankenphp-worker-audit/references/rules.md
var rulesReference string

// RuleDoc returns the reference section of a rule (case-insensitive), without its heading line.
func RuleDoc(id string) (string, bool) {
	for _, section := range ruleSections() {
		if strings.EqualFold(section.id, id) {
			return section.body, true
		}
	}
	return "", false
}

// RuleIDs lists the rules of the reference, in its order.
func RuleIDs() []string {
	var ids []string
	for _, section := range ruleSections() {
		ids = append(ids, section.id)
	}
	return ids
}

type ruleSection struct {
	id    string
	title string
	body  string
}

func ruleSections() []ruleSection {
	var sections []ruleSection
	for _, chunk := range strings.Split(strings.ReplaceAll(rulesReference, "\r\n", "\n"), "\n## ")[1:] {
		heading, body, _ := strings.Cut(chunk, "\n")
		id, _, _ := strings.Cut(heading, " ")
		sections = append(sections, ruleSection{id: id, title: heading, body: strings.TrimSpace(body)})
	}
	return sections
}
