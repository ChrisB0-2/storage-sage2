# Changelog

All notable changes to Storage-Sage will be documented in this file.

## [Unreleased]

### Changed

- **Daemon shutdown now waits for in-flight runs**: The daemon now tracks all active cleanup runs (both scheduled and API-triggered via `/trigger`). During shutdown, it waits for these runs to complete before closing resources like the auditor. This prevents "database closed" errors when shutdown occurs during an active run.

- **Bounded shutdown with configurable timeout**: The wait for in-flight runs has a configurable timeout (default: 10s). If runs don't complete within this window, shutdown proceeds anyway to prevent indefinite hangs. A warning is logged when timeout is hit.

- **New `RunWaitTimeout` config option**: Added `RunWaitTimeout` to `daemon.Config` to configure how long shutdown waits for in-flight runs (default: 10s).

### Fixed

- Fixed potential race condition where the auditor could be closed while a `/trigger` API request was still writing to it.

- Made scheduled run tracking panic-safe by using deferred cleanup of the run tracking WaitGroup.

### Improved

- Replaced several `time.Sleep`-based test synchronizations with deterministic channel/polling-based patterns to reduce test flakiness.

- Added `waitForState` test helper for more reliable test synchronization.

### Added

- New tests:
  - `TestDaemon_AuditorWaitsForInFlightTriggerRun`: Proves auditor close waits for in-flight API runs
  - `TestDaemon_RunWaitTimeout`: Verifies bounded shutdown behavior when runs exceed timeout
