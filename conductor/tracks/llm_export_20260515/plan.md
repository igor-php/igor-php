# Implementation Plan: LLM Export Feature

## Phase 1: Define LLM JSON Structure & Reporter Foundation [checkpoint: beac17a]
- [x] Task: Create data structures (structs) for the LLM JSON output (Warning, Context, Metadata). (8ee742e)
    - [ ] Write unit tests for JSON serialization of the new structures.
    - [ ] Implement the Go structs with appropriate JSON tags.
- [ ] Task: Create a new `LLMReporter` that implements the existing reporter interface.
    - [ ] Write unit tests to ensure `LLMReporter` can receive warnings and buffer them.
    - [ ] Implement the `LLMReporter` logic.
- [x] Task: Conductor - User Manual Verification 'Phase 1: Define LLM JSON Structure & Reporter Foundation' (Protocol in workflow.md) (beac17a)

## Phase 2: Context Extraction (AST & Snippet) [checkpoint: 6cf6fa1]
- [x] Task: Implement AST node stringification/extraction. (ee7e900)
    - [ ] Write tests for extracting AST node details (type, bounds) from Tree-sitter nodes.
    - [ ] Implement the AST detail extraction logic in the analyzer.
- [x] Task: Implement Code Snippet extraction. (ee7e900)
    - [ ] Write tests to extract specific lines of code based on warning coordinates.
    - [ ] Implement the snippet extraction logic.
- [x] Task: Conductor - User Manual Verification 'Phase 2: Context Extraction (AST & Snippet)' (Protocol in workflow.md) (6cf6fa1)

## Phase 3: Dependencies & Service Context [checkpoint: 52c23e1]
- [x] Task: Expose dependency context in the analyzer output. (21527ac)
    - [ ] Write tests to ensure service/class dependencies are properly attached to warnings.
    - [ ] Update the `auditor.go` or `visitor.go` to attach context to detected mutations.
- [ ] Task: Map extracted context into the `LLMReporter`.
    - [ ] Write tests verifying that `LLMReporter` correctly structures AST, Snippet, and Dependency data into the JSON payload.
    - [ ] Implement mapping logic in `LLMReporter`.
- [x] Task: Conductor - User Manual Verification 'Phase 3: Dependencies & Service Context' (Protocol in workflow.md) (52c23e1)

## Phase 4: CLI Integration
- [ ] Task: Add CLI option for LLM output.
    - [ ] Write tests for the CLI flag parsing (`--output=llm`).
    - [ ] Implement the CLI flag in `config.go` and wire it to instantiate `LLMReporter`.
- [ ] Task: E2E Integration Test.
    - [ ] Create an integration test fixture that runs `igor-php` with `--output=llm` and validates the JSON output.
- [ ] Task: Conductor - User Manual Verification 'Phase 4: CLI Integration' (Protocol in workflow.md)