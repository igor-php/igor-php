package main

import (
	"testing"
)

func TestNormalizeClassName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"\\App\\Controller\\Main", "App\\Controller\\Main"},
		{"App\\Service\\Stateless", "App\\Service\\Stateless"},
		{"\\\\ManyBackslashes", "ManyBackslashes"},
		{"", ""},
	}

	for _, test := range tests {
		result := NormalizeClassName(test.input)
		if result != test.expected {
			t.Errorf("NormalizeClassName(%q) = %q; want %q", test.input, result, test.expected)
		}
	}
}

func TestLaravelBridgeStub(t *testing.T) {
	lb := NewLaravelBridge("/tmp", Config{})
	if lb.GetName() != "Laravel" {
		t.Errorf("Expected GetName to be 'Laravel', got %s", lb.GetName())
	}
	if len(lb.GetDefinitions()) != 0 {
		t.Errorf("Expected GetDefinitions to be empty, got %v", lb.GetDefinitions())
	}
	if !lb.IsSharedService("AnyClass") {
		t.Errorf("Expected IsSharedService to be true in stub")
	}
}
