package main

import (
	"reflect"
	"testing"

	"github.com/igor-php/igor-php/pkg/symbol"
)

func TestExtractDependencies(t *testing.T) {
	tests := []struct {
		name     string
		def      symbol.SymfonyService
		expected []string
	}{
		{
			name: "Structured service reference",
			def: symbol.SymfonyService{
				Arguments: []any{
					map[string]any{"type": "service", "id": "logger"},
					map[string]any{"type": "service", "id": "doctrine.orm.entity_manager"},
					"not a service",
				},
			},
			expected: []string{"logger", "doctrine.orm.entity_manager"},
		},
		{
			name: "String prefixed service reference",
			def: symbol.SymfonyService{
				Arguments: []any{
					"@logger",
					"@app.my_service",
					"plain_string",
				},
			},
			expected: []string{"logger", "app.my_service"},
		},
		{
			name: "Mixed formats",
			def: symbol.SymfonyService{
				Arguments: []any{
					map[string]any{"type": "service", "id": "service_1"},
					"@service_2",
				},
			},
			expected: []string{"service_1", "service_2"},
		},
		{
			name: "Empty arguments",
			def: symbol.SymfonyService{
				Arguments: []any{},
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDependencies(tt.def)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("extractDependencies() = %v, want %v", got, tt.expected)
			}
		})
	}
}
