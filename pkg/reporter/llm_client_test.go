package reporter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLLMClient_Review(t *testing.T) {
	// 1. Mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}

		// Verify body
		var req ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
		}
		if req.Model != "test-model" {
			t.Errorf("Expected test-model, got %s", req.Model)
		}

		// Respond
		resp := ChatCompletionResponse{
			Choices: []struct {
				Message Message `json:"message"`
			}{
				{Message: Message{Role: "assistant", Content: "This is a review."}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 2. Initialize client
	client := NewLLMClient(server.URL, "test-key", "test-model")

	// 3. Call Review
	result, err := client.Review("Hello")
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}

	if result != "This is a review." {
		t.Errorf("Expected 'This is a review.', got '%s'", result)
	}
}
