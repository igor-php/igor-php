package fixer

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// resolveImport returns how the file should refer to fqcn, adding a use
// statement when that is unambiguous and falling back to the FQCN otherwise.
func (f *fixer) resolveImport(fqcn string) string {
	if name, ok := f.importNames[fqcn]; ok {
		return name
	}
	if f.importNames == nil {
		f.importNames = make(map[string]string)
	}
	name := f.computeImport(fqcn)
	f.importNames[fqcn] = name
	return name
}

func (f *fixer) computeImport(fqcn string) string {
	qualified := `\` + fqcn
	for alias, target := range f.imports {
		if strings.EqualFold(target, fqcn) {
			return alias
		}
	}
	short := fqcn[strings.LastIndex(fqcn, `\`)+1:]
	if f.shortNameTaken(short) {
		return qualified
	}
	at, prefix, suffix, ok := f.importInsertion()
	if !ok {
		return qualified
	}
	f.edits = append(f.edits, edit{start: at, end: at, text: prefix + "use " + fqcn + ";" + suffix})
	return short
}

// shortNameTaken reports whether short already names another import or a
// class-like declared in the file.
func (f *fixer) shortNameTaken(short string) bool {
	for alias := range f.imports {
		if strings.EqualFold(alias, short) {
			return true
		}
	}
	taken := false
	walk(f.root, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration":
			if strings.EqualFold(f.text(n.ChildByFieldName("name")), short) {
				taken = true
			}
		case "namespace_use_declaration":
			if strings.Contains(f.text(n), "{") && strings.Contains(f.text(n), short) {
				taken = true
			}
		}
		return true
	})
	return taken
}

// importInsertion finds where to add a use statement. It only supports files
// with a single, unbracketed namespace.
func (f *fixer) importInsertion() (uint, string, string, bool) {
	var namespace, lastUse *sitter.Node
	namespaces := 0
	for i := uint(0); i < f.root.ChildCount(); i++ {
		child := f.root.Child(i)
		switch child.Kind() {
		case "namespace_definition":
			namespaces++
			namespace = child
			if child.ChildByFieldName("body") != nil {
				return 0, "", "", false
			}
		case "namespace_use_declaration":
			if namespaces > 0 {
				lastUse = child
			}
		case "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration", "function_definition":
			i = f.root.ChildCount()
		}
	}
	if namespaces != 1 {
		return 0, "", "", false
	}
	if lastUse != nil {
		return lastUse.EndByte(), f.newline, "", true
	}
	// Keep a blank line between the new use statement and the code that follows.
	suffix := ""
	if next := namespace.NextSibling(); next != nil && !f.blankLineBetween(namespace.EndByte(), next.StartByte()) {
		suffix = f.newline
	}
	return namespace.EndByte(), f.newline + f.newline, suffix, true
}

// blankLineBetween reports whether the text between two offsets holds an empty line.
func (f *fixer) blankLineBetween(start, end uint) bool {
	return strings.Count(strings.ReplaceAll(string(f.content[start:end]), "\r", ""), "\n") >= 2
}

// collectImports maps the local alias of each class import to its FQCN.
func (f *fixer) collectImports() map[string]string {
	imports := make(map[string]string)
	for i := uint(0); i < f.root.ChildCount(); i++ {
		decl := f.root.Child(i)
		if decl.Kind() != "namespace_use_declaration" || strings.Contains(f.text(decl), "{") {
			continue
		}
		isClassImport := true
		for j := uint(0); j < decl.ChildCount(); j++ {
			if kind := decl.Child(j).Kind(); kind == "function" || kind == "const" {
				isClassImport = false
			}
		}
		if !isClassImport {
			continue
		}
		for j := uint(0); j < decl.ChildCount(); j++ {
			clause := decl.Child(j)
			if clause.Kind() != "namespace_use_clause" {
				continue
			}
			fields := strings.Fields(f.text(clause))
			if len(fields) == 0 {
				continue
			}
			target := strings.TrimPrefix(fields[0], `\`)
			alias := target[strings.LastIndex(target, `\`)+1:]
			if len(fields) == 3 && strings.EqualFold(fields[1], "as") {
				alias = fields[2]
			}
			imports[alias] = target
		}
	}
	return imports
}
