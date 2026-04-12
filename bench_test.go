package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkAuditor(b *testing.B) {
	tmpDir, err := ioutil.TempDir("", "igor_bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Generate 100 dummy files
	for i := 0; i < 100; i++ {
		content := fmt.Sprintf(`<?php
class Service%d {
    private $prop;
    public function set($v) { $this->prop = $v; }
}`, i)
		err := ioutil.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("service%d.php", i)), []byte(content), 0644)
		if err != nil {
			b.Fatal(err)
		}
	}

	cfg := Config{}
	auditor := NewServiceAuditor(cfg)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		files, _ := filepath.Glob(filepath.Join(tmpDir, "*.php"))
		for _, f := range files {
			_, _ = auditor.Audit(f)
		}
	}
}
