# Implementation Plan: Go Project Restructuring

## Phase 1: Setup Directory Structure [checkpoint: 26f44f2]
- [x] Task: Create the new directory skeleton (`cmd/igor`, `internal/analyzer`, `internal/auditor`, `internal/config`, `internal/tree-sitter`, `pkg/reporter`, `pkg/symbol`, `api`, `assets`, `scripts`). bb2e870
- [x] Task: Conductor - User Manual Verification 'Setup Directory Structure' (Protocol in workflow.md) 26f44f2

## Phase 2: Relocate Non-Go Files [checkpoint: 1ff0e09]
- [x] Task: Move `igor-php.png` and `review.png` to `assets/`. e9a999c
- [x] Task: Move `zigcc_wrapper.sh` and `zigcxx_wrapper.sh` to `scripts/`. f5f66e8
- [x] Task: Rename `tests/` directory to `test/` to match the target architecture. 33b2012
- [x] Task: Conductor - User Manual Verification 'Relocate Non-Go Files' (Protocol in workflow.md) 1ff0e09

## Phase 3: Relocate Core Code and Update Packages
- [x] Task: Move symbol models (`models.go`) to `pkg/symbol/` and update package name. a4105d0
- [x] Task: Move reporter logic (`reporter.go`, `reporter_test.go`) to `pkg/reporter/` and update package name. 29bdcf2
- [~] Task: Move configuration logic (`config.go`, `config_test.go`) to `internal/config/` and update package name.
- [ ] Task: Move Symfony auditor logic (`auditor.go`, `service_auditor_test.go`, `symfony.go`) to `internal/auditor/` and update package name.
- [ ] Task: Move AST/Mutation logic (`visitor.go` and similar) to `internal/analyzer/` and update package name.
- [ ] Task: Identify and move Tree-sitter specific logic to `internal/tree-sitter/`.
- [ ] Task: Relocate remaining root files (e.g., `composer.go`, `baseline.go`, `baseline_test.go`) to appropriate `internal/` subdirectories based on responsibility.
- [ ] Task: Move `main.go`, `init.go`, and related entry point logic to `cmd/igor/`.
- [ ] Task: Conductor - User Manual Verification 'Relocate Core Code and Update Packages' (Protocol in workflow.md)

## Phase 4: Fix Imports, Build, and Verify
- [ ] Task: Update all internal imports across the project to reference the new package paths.
- [ ] Task: Create a `Makefile` with basic `build`, `test`, and `lint` commands.
- [ ] Task: Run `go mod tidy` and resolve any module issues.
- [ ] Task: Run `go test ./...` and fix any failing tests due to the restructuring.
- [ ] Task: Verify the build with `go build -o bin/igor ./cmd/igor` and ensure the binary functions correctly.
- [ ] Task: Conductor - User Manual Verification 'Fix Imports, Build, and Verify' (Protocol in workflow.md)