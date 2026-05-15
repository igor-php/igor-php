# Product Guidelines

## 1. User Experience (UX)
- **Speed is Paramount:** The tool must execute near-instantly to avoid slowing down developer workflows.
- **Actionable Feedback:** Error messages should clearly identify the mutated property, the location in the code, and provide a direct hint for remediation (e.g., `💡 Hint: Add '$this->prop = null;'`).
- **Zero Configuration First:** The tool must work out-of-the-box for typical Symfony projects with minimal to no configuration.

## 2. CLI Interface & Output
- **Clarity over Verbosity:** Present findings concisely. Avoid outputting tracebacks unless a verbose/debug flag is passed.
- **Colored Output:** Use colors to distinguish between severities (e.g., Red for Errors, Yellow for Warnings, Green for Success).
- **Machine-Readable Support:** Always provide structured output formats (SARIF, LLM-optimized JSON) alongside the default human-readable text for CI/CD and automation.

## 3. Code & Architecture (Product Level)
- **Zero Dependencies for End-Users:** Distribute the linter as a single compiled binary without requiring a PHP or Node.js runtime environment on the target machine.
- **No False Positives:** Precision is more important than covering every possible edge case. If a check is prone to false positives, it should be disabled or marked as a warning until refined.