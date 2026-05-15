package reporter

import (
	"fmt"
	"os"
)

const FrictionlessPromptTemplate = `You are a Senior Backend Security Engineer and PHP Expert.
I have used Igor-PHP to audit a Symfony application for state-related leaks when running in Worker Mode (e.g., FrankenPHP, RoadRunner).

Below is a JSON export of the warnings found.
Please triage these warnings and for each one:
1. Explain why this is dangerous in a long-running worker environment.
2. Suggest a fix (e.g., using ResetInterface, making it stateless, or using a factory).
3. Rate the severity (Low, Medium, High, Critical).

JSON PAYLOAD:
%s
`

func GenerateFrictionlessPrompt(jsonPayload string) error {
	content := fmt.Sprintf(FrictionlessPromptTemplate, jsonPayload)
	err := os.WriteFile("igor-review-prompt.md", []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("failed to write prompt file: %w", err)
	}
	return nil
}
