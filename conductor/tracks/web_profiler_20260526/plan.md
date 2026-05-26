# Implementation Plan: Integrate Igor-PHP with Symfony Web Profiler

## Phase 1: Data Collection & Integration
- [x] Task: Create IgorDataCollector (b00c16f)
    - [x] Write Tests: Unit tests for `IgorDataCollector` to ensure it formats audit data correctly.
    - [x] Implement Feature: Create the `IgorDataCollector` class implementing `DataCollectorInterface`.
- [x] Task: Integrate Go Binary Execution (78d1335)
    - [x] Write Tests: Mock the Go binary execution to test the bundle's handling of JSON output.
    - [x] Implement Feature: Execute the `igor-php` binary (or read the generated JSON report) within the collector to gather live data.
- [ ] Task: Conductor - User Manual Verification 'Phase 1: Data Collection & Integration' (Protocol in workflow.md)

## Phase 2: Web Profiler UI
- [ ] Task: Create Toolbar Icon
    - [ ] Implement Feature: Design and implement the Twig template for the Web Profiler toolbar (SVG icon, status colors).
- [ ] Task: Create Detailed Panel
    - [ ] Implement Feature: Design and implement the detailed Twig template for the Web Profiler panel showing the list of leaks and recommendations.
- [ ] Task: Bundle Configuration
    - [ ] Implement Feature: Update `IgorPhpBundle` configuration to correctly load the Twig templates and register the DataCollector only in the `dev` environment.
- [ ] Task: Conductor - User Manual Verification 'Phase 2: Web Profiler UI' (Protocol in workflow.md)