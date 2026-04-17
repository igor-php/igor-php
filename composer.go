package main

import (
        "encoding/json"
        "os"
        "path/filepath"
        "sort"
)

// ComposerJSON represents a minimal composer.json structure.
type ComposerJSON struct {
        Require    map[string]string `json:"require"`
        RequireDev map[string]string `json:"require-dev"`
}

// ParseComposerDev parses composer.json and returns a list of packages in require-dev.
func ParseComposerDev(root string) ([]string, error) {
        data, err := os.ReadFile(filepath.Join(root, "composer.json"))
        if err != nil {
                if os.IsNotExist(err) {
                        return []string{}, nil
                }
                return nil, err
        }

        var composer ComposerJSON
        if err := json.Unmarshal(data, &composer); err != nil {
                return nil, err
        }

        devPackages := make([]string, 0, len(composer.RequireDev))
        for pkg := range composer.RequireDev {
                devPackages = append(devPackages, pkg)
        }
        sort.Strings(devPackages)

        return devPackages, nil
}
