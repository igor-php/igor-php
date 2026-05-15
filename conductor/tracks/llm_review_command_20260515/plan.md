# Implementation Plan: LLM Review Command

## Phase 1: Review Command Setup & Frictionless Mode
- [x] Task: Create the new `review` CLI subcommand structure.
    - [x] Write a test verifying that `igor-php review <invalid_file>` returns an appropriate error.
    - [x] Implement the `review` subcommand parsing in `cmd/igor/main.go` or a new dedicated file.
- [x] Task: Implement the Frictionless Mode prompt generation.
    - [x] Write a test to verify `igor-review-prompt.md` is generated with the correct "Senior Backend Security Engineer" system prompt and JSON payload.
    - [x] Implement the logic to read the JSON file, format the markdown, and write the file.
- [x] Task: Implement the "Witty Igor" terminal output for Frictionless Mode.
    - [x] Write a test capturing stdout to verify the specific message `📄 Prompt ready! Copy the content...` is printed.
    - [x] Implement the terminal output logic.
- [x] Task: Conductor - User Manual Verification 'Phase 1: Review Command Setup & Frictionless Mode' (Protocol in workflow.md)

## Phase 2: Configuration for Expert Mode
- [x] Task: Update the `igor.json` config schema to support LLM provider settings.
    - [x] Write tests verifying that LLM configuration (e.g., `llm_api_url`, `llm_api_key_env`, `llm_model`) can be parsed from `igor.json` or ENV.
    - [x] Update `internal/config/models.go` and the loading logic.
- [x] Task: Conductor - User Manual Verification 'Phase 2: Configuration for Expert Mode' (Protocol in workflow.md)

## Phase 3: Expert Mode API Integration
- [x] Task: Create a simple HTTP client to interact with an OpenAI-compatible API.
    - [x] Write a test with a mocked HTTP server to verify the payload is sent correctly and the response is parsed.
    - [x] Implement the LLM API client.
- [x] Task: Wire Expert Mode into the `review` command.
    - [x] Write an integration test (with mocked API) verifying that if configured, `igor-php review` calls the API, prints to stdout, and saves `igor-review.md`.
    - [x] Implement the execution routing (Frictionless vs. Expert) based on configuration.
- [x] Task: Conductor - User Manual Verification 'Phase 3: Expert Mode API Integration' (Protocol in workflow.md)
