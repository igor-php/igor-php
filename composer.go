package main

import (
        "encoding/json"
        "os"
        "path/filepath"
        "sort"
        "strings"
)

// ComposerJSON represents a minimal composer.json structure.
type ComposerJSON struct {
        Require    map[string]string `json:"require"`
        RequireDev map[string]string `json:"require-dev"`
}

// ParseComposer parses composer.json and returns lists of production and dev packages.
func ParseComposer(root string) (prod []string, dev []string, err error) {
        data, err := os.ReadFile(filepath.Join(root, "composer.json"))
        if err != nil {
                if os.IsNotExist(err) {
                        return []string{}, []string{}, nil
                }
                return nil, nil, err
        }

        var composer ComposerJSON
        if err := json.Unmarshal(data, &composer); err != nil {
                return nil, nil, err
        }

        prod = make([]string, 0, len(composer.Require))
        for pkg := range composer.Require {
                if pkg != "php" && !strings.HasPrefix(pkg, "ext-") {
                        prod = append(prod, pkg)
                }
        }
        sort.Strings(prod)

        dev = make([]string, 0, len(composer.RequireDev))
        for pkg := range composer.RequireDev {
                dev = append(dev, pkg)
        }
        sort.Strings(dev)

        return prod, dev, nil
}
