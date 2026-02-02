# Changelog

All notable changes to Storage-Sage will be documented in this file.

## [v0.3.0] - 2026-02-02

### Added

- **`init` command**: New `storage-sage init` command for first-time setup that creates config file, audit database, and trash directory with sensible defaults.

- **Web UI**: React dashboard frontend with real-time status, logs, and controls. Access at `http://localhost:8080` when running in daemon mode.

- **Trash CLI commands**: Full trash management via CLI:
  - `storage-sage trash list` - List trashed files with metadata
  - `storage-sage trash restore` - Restore files to original location
  - `storage-sage trash empty` - Permanently delete old trash items

- **PID file support**: Single-instance enforcement via `-pid-file` flag prevents multiple daemon instances.

- **Soft-delete mode**: Move files to trash instead of permanent deletion via `-trash-path` flag.

- **API authentication**: API key authentication and RBAC for HTTP API endpoints.

- **Grafana integration**: Documentation and example dashboards for Grafana/Loki monitoring.

- **Systemd integration**: Example unit file for running as a system service.

### Changed

- **Daemon shutdown now waits for in-flight runs**: The daemon now tracks all active cleanup runs (both scheduled and API-triggered via `/trigger`). During shutdown, it waits for these runs to complete before closing resources like the auditor. This prevents "database closed" errors when shutdown occurs during an active run.

- **Bounded shutdown with configurable timeout**: The wait for in-flight runs has a configurable timeout (default: 10s). If runs don't complete within this window, shutdown proceeds anyway to prevent indefinite hangs. A warning is logged when timeout is hit.

- **New `RunWaitTimeout` config option**: Added `RunWaitTimeout` to `daemon.Config` to configure how long shutdown waits for in-flight runs (default: 10s).

- **Go 1.24**: Updated CI to Go 1.24.

### Fixed

- Fixed Windows build by adding platform-specific disk usage detection.

- Fixed gosec G115 integer overflow warnings in disk usage calculations.

- Fixed cross-platform build issues with uint64 conversion for stat.Dev.

- Fixed Loki logger test flakiness with deterministic synchronization.

- Fixed config API JSON serialization issues.

- Fixed mount boundary enforcement in scanner.

- Fixed potential race condition where the auditor could be closed while a `/trigger` API request was still writing to it.

- Made scheduled run tracking panic-safe by using deferred cleanup of the run tracking WaitGroup.

- Fixed critical security and concurrency issues identified during hardening.

### Improved

- Replaced several `time.Sleep`-based test synchronizations with deterministic channel/polling-based patterns to reduce test flakiness.

- Added `waitForState` test helper for more reliable test synchronization.

- Improved daemon API robustness and configurability.

- Scanner resilience improvements.

## [Unreleased]
