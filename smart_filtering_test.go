package main

import (
        "os"
        "path/filepath"
        "testing"
)

func TestSmartFiltering(t *testing.T) {
        tmpDir, err := os.MkdirTemp("", "igor_filtering_test")
        if err != nil {
                t.Fatal(err)
        }
        defer func() { _ = os.RemoveAll(tmpDir) }()

        // Create a mock vendor structure
        devPkgPath := filepath.Join(tmpDir, "vendor", "phpunit", "phpunit", "src")
        if err := os.MkdirAll(devPkgPath, 0755); err != nil { t.Fatal(err) }
        
        devFile := filepath.Join(devPkgPath, "TestCase.php")
        if err := os.WriteFile(devFile, []byte("<?php class TestCase {}"), 0644); err != nil {
                t.Fatal(err)
        }

        prodPkgPath := filepath.Join(tmpDir, "vendor", "symfony", "framework-bundle")
        if err := os.MkdirAll(prodPkgPath, 0755); err != nil { t.Fatal(err) }
        prodFile := filepath.Join(prodPkgPath, "Kernel.php")
        if err := os.WriteFile(prodFile, []byte("<?php class Kernel {}"), 0644); err != nil {
                t.Fatal(err)
        }

        cfg := Config{
                DevPackages: []string{"phpunit/phpunit"},
        }
        auditor := NewAuditor(cfg)

        t.Run("IsDevPackagePath detection", func(t *testing.T) {
                if !auditor.IsDevPackagePath(devFile) {
                        t.Errorf("Expected %s to be recognized as dev package path", devFile)
                }
                if auditor.IsDevPackagePath(prodFile) {
                        t.Errorf("Expected %s NOT to be recognized as dev package path", prodFile)
                }
        })

        t.Run("collectSymfonyServices should skip dev packages", func(t *testing.T) {
                auditor.Symfony = &SymfonyBridge{
                        Container: &SymfonyContainer{
                                Definitions: map[string]SymfonyService{
                                        "phpunit.test_case": {Class: "PHPUnit\\TestCase", Public: true, Shared: true},
                                        "symfony.kernel":    {Class: "Symfony\\Kernel", Public: true, Shared: true},
                                },
                        },
                        ClassToFile: map[string]string{
                                "PHPUnit\\TestCase": devFile,
                                "Symfony\\Kernel":    prodFile,
                        },
                }

                processed := make(map[string]bool)
                list := collectSymfonyServices(cfg, auditor, processed)

                foundDev := false
                foundProd := false
                for _, item := range list {
                        if item.FilePath == devFile {
                                foundDev = true
                        }
                        if item.FilePath == prodFile {
                                foundProd = true
                        }
                }

                if foundDev {
                        t.Error("Dev package service was NOT skipped")
                }
                if !foundProd {
                        t.Error("Prod package service was skipped")
                }
        })
}
