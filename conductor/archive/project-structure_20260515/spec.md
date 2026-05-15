# Track: Go Project Restructuring (Refactor)

## Overview
The goal of this track is to refactor the `igor-php` project structure from a flat layout to a standard, professional open-source Go project layout. This will improve maintainability, separation of concerns, and prepare the project for future extensibility (e.g., exposing public packages or adding new tools).

## Functional Requirements
The current source files in the root directory will be moved to a structured layout based on the provided specification.

### Target Architecture
```text
igor-php/
├── cmd/
│   └── igor/
│       └── main.go       # Point d'entrée unique. Juste le parsing des flags et l'appel au moteur.
├── internal/             # Code privé, non importable par d'autres projets.
│   ├── analyzer/         # Le cœur : visiteur AST, détection des mutations.
│   ├── auditor/          # Logique métier : comparaison avec le container Symfony.
│   ├── config/           # Gestion de la configuration et des flags.
│   └── tree-sitter/      # Abstraction pour la manipulation des nœuds PHP.
├── pkg/                  # Code public que d'autres projets pourraient importer.
│   ├── reporter/         # Interface Reporter et implémentations (CLI, JSON, LLM).
│   └── symbol/           # Structures de données pour les classes/services.
├── api/                  # (Placeholder) Pour une future API HTTP pour Igor-Radar.
├── assets/               # Images pour le README, logos, etc. (Déjà existant: igor-php.png, review.png)
├── scripts/              # Scripts pour le build ou le téléchargement de Tree-sitter (Déjà existant: zigcc_wrapper.sh, zigcxx_wrapper.sh)
├── test/                 # Fixtures de tests (fichiers PHP de test) et tests E2E. (Déjà existant dans tests/)
├── go.mod
├── go.sum
└── Makefile              # Commandes pour build, test, et lint.
```

### Key Refactoring Actions
1.  **Entrypoint (`cmd/igor/main.go`)**: Move `main.go` into `cmd/igor/`. It should only contain flag parsing and the main execution engine call.
2.  **Core Logic (`internal/`)**:
    *   Move AST visiting and mutation detection logic (e.g., `visitor.go`) to `internal/analyzer`.
    *   Move Symfony container comparison and auditing logic (e.g., `auditor.go`, `symfony.go`) to `internal/auditor`.
    *   Move configuration handling (e.g., `config.go`) to `internal/config`.
    *   Move Tree-sitter abstraction logic to `internal/tree-sitter`.
3.  **Public Packages (`pkg/`)**:
    *   Move reporter interfaces and implementations (e.g., `reporter.go`) to `pkg/reporter`.
    *   Move symbol data structures (e.g., `models.go`) to `pkg/symbol`.
4.  **Tests**: Unit test files (`*_test.go`) must remain colocated with their corresponding `.go` files in the new directories.
5.  **Assets & Scripts**: Move `igor-php.png`, `review.png`, `zigcc_wrapper.sh`, and `zigcxx_wrapper.sh` to their respective `assets/` and `scripts/` directories.
6.  **Makefile**: Create a basic `Makefile` for standard Go commands (`build`, `test`, `lint`).
7.  **Package Naming**: Update package declarations in all moved files to match their new directory names (e.g., `package analyzer`, `package reporter`), and resolve any import path issues.

## Non-Functional Requirements
-   All existing unit tests and integration tests must pass after the restructuring.
-   The build process must still produce a working executable.

## Acceptance Criteria
-   The directory structure matches the target architecture.
-   Running `go test ./...` passes without errors.
-   Running `go build ./cmd/igor/...` produces a working binary.
-   No core source code files (`.go`) are present in the root directory.