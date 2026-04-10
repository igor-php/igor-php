package main

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
)

func loadConfig(root string) Config {
	c := Config{
		Exclude: []string{"vendor", "var", "cache", "tests", "Entity", "Dto", "ApiResource"},
		SafeNamespaces: []string{
			"Symfony\\",
			"Doctrine\\",
		},
	}
	data, err := ioutil.ReadFile(filepath.Join(root, "igor.json"))
	if err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c
}
