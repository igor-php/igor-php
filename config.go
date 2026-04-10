package main

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
)

func loadConfig(root string) Config {
	c := Config{Exclude: []string{"vendor", "var", "cache", "tests"}}
	data, err := ioutil.ReadFile(filepath.Join(root, ".fcheck.json"))
	if err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c
}
