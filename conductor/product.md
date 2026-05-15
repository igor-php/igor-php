# Initial Concept
Build an ultra-fast static linter in Go (`igor-php`) that prepares Symfony applications for the persistent memory model of FrankenPHP by detecting state mutations and preventing workers from crashing.

# Target Audience & Integrations
The primary users are PHP/Symfony developers who are migrating to or maintaining applications on FrankenPHP. The tool is designed to be run as a Local CLI Tool during active development and integrated natively into CI/CD Pipelines to gate code quality before deployment.

# Core Rules & Roadmap
`igor-php` focuses heavily on ensuring safety in a persistent memory context. Key checks include detecting complex state mutations (`??=`, `list()`, increments) and dangerous termination calls (`exit()`, `die()`). 
The immediate roadmap prioritizes auditing the Symfony Dependency Injection container to verify that every loaded service is strictly stateless, ensuring zero state-leakage across requests.

# Reporting & Extensibility
The linter will provide Colored CLI Output for immediate, human-readable feedback during local development. Additionally, it will support SARIF Format and structured LLM-optimized JSON output to allow deep integration with CI/CD platforms (like GitHub Actions) and automated triage via Large Language Models.
To maintain its extreme performance profile, the tool will rely exclusively on Core-only Rules hardcoded in Go, rather than supporting a dynamic user-configurable rule engine.