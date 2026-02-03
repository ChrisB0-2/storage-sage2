# Storage-Sage Codebase Analysis & Creator Cheatsheet

A comprehensive reference for understanding the internals of Storage-Sage.

---

## File Structure Overview

```
storage-sage/
├── cmd/storage-sage/          # Entry point & CLI
│   ├── main.go                # CLI parsing, subcommands, pipeline orchestration
│   └── main_test.go
│
├── internal/
│   ├── core/                  # Domain contracts (interfaces & types)
│   ├── config/                # YAML config loading & validation
│   ├── scanner/               # Filesystem traversal
│   ├── policy/                # Deletion eligibility rules
│   ├── safety/                # Protection & symlink detection
│   ├── planner/               # Plan generation from candidates
│   ├── executor/              # TOCTOU-safe deletion
│   ├── auditor/               # JSONL + SQLite audit logging
│   ├── daemon/                # Long-running service & HTTP API
│   ├── trash/                 # Soft-delete implementation
│   ├── logger/                # Structured logging + Loki
│   ├── metrics/               # Prometheus instrumentation
│   ├── auth/                  # API authentication & RBAC
│   ├── notifier/              # Webhook notifications
│   ├── pidfile/               # Single-instance enforcement
│   └── web/                   # Embedded frontend assets
│
├── web/src/                   # React TypeScript frontend
│   ├── api/                   # API client & types
│   ├── components/            # Reusable UI components
│   ├── pages/                 # Dashboard, History, Trash, etc.
│   └── hooks/                 # Custom React hooks
│
├── deploy/                    # Deployment configs
│   ├── systemd/               # Service files
│   ├── prometheus.yml         # Scrape config
│   └── grafana/               # Dashboards & provisioning
│
└── dashboards/                # Grafana JSON + alerting rules
```

---

## Package-by-Package Breakdown

### `internal/core` — Domain Contracts
**Files:** `contracts.go`, `audit_helpers.go`

**Purpose:** Pure interfaces and types. Zero dependencies on other internal packages.

| Type | Purpose |
|------|---------|
| `Candidate` | Filesystem object with metadata (path, size, modtime, device ID, symlink info) |
| `Decision` | Policy evaluation result (Allow, Reason, Score) |
| `SafetyVerdict` | Safety check result (Allowed, Reason) |
| `PlanItem` | Candidate + Decision + SafetyVerdict |
| `ActionResult` | Execution outcome (Deleted, BytesFreed, Reason) |
| `AuditEvent` | Structured log entry with nested Fields map |

| Interface | Implementors |
|-----------|--------------|
| `Scanner` | `scanner.WalkDirScanner` |
| `Policy` | `policy.Age`, `Size`, `Extension`, `Exclusion`, `Composite` |
| `Safety` | `safety.Engine` |
| `Planner` | `planner.Simple` |
| `Deleter` | `executor.Simple` |
| `Auditor` | `auditor.JSONL`, `SQLite`, `Multi` |
| `Metrics` | `metrics.Prometheus`, `Noop` |

**Design Decision:** Interface-first design enables testing via mocks and loose coupling between components.

---

### `internal/config` — Configuration Management
**Files:** `config.go`, `validate.go`

**Key Types:**
```go
type Config struct {
    Scan          ScanConfig
    Policy        PolicyConfig
    Safety        SafetyConfig
    Execution     ExecutionConfig
    Logging       LoggingConfig
    Daemon        DaemonConfig
    Metrics       MetricsConfig
    Auth          AuthConfig
    Notifications NotificationsConfig
}
```

**Key Functions:**
- `Default()` — Sensible defaults
- `Load(path)` — YAML parsing
- `LoadOrDefault(path)` — Load if exists, else defaults
- `FindConfigFile()` — Search `~/.config`, `/etc`, cwd
- `Validate(cfg)` — Structural validation
- `ValidateFinal(cfg)` — Full validation after CLI merge

**Design Decision:** Nested config objects with layered validation. CLI flags override config file values for flexibility.

---

### `internal/scanner` — Filesystem Traversal
**Files:** `walkdir.go`, `device_unix.go`, `device_other.go`

```go
func (s *WalkDirScanner) Scan(ctx, req) (<-chan Candidate, <-chan error)
```

**Features:**
- Uses `filepath.WalkDir` (Go 1.16+) for efficiency
- Returns **channels** for non-blocking streaming
- Captures **device ID** per file for mount boundary detection
- Uses `lstat` (never follows symlinks)
- Respects `MaxDepth` configuration
- Emits metrics: files/dirs scanned, scan duration

**Design Decision:** Fail-soft behavior — logs and skips inaccessible paths instead of aborting the entire scan.

---

### `internal/policy` — Deletion Eligibility Rules
**Files:** `age.go`, `size.go`, `extension.go`, `exclusion.go`, `composite.go`, `stub.go`

| Policy | Logic | Score Formula |
|--------|-------|---------------|
| `AgePolicy` | `modtime < now - min_age` | `(days × 10) + size_MB` |
| `SizePolicy` | `size >= min_bytes` | `size_MB` (capped at 1024) |
| `ExtensionPolicy` | `ext in allowed_list` | passthrough |
| `ExclusionPolicy` | `!matches(glob_pattern)` | passthrough |
| `CompositePolicy` | AND/OR combination | AND: min score, OR: max score |

**Design Decision:** Strategy pattern for pluggable policies. Composite pattern enables flexible AND/OR rule chaining without code changes. Default combination: Age AND (Size OR Extension) AND NOT Exclusion.

---

### `internal/safety` — Protection Engine
**Files:** `safety.go`, `ancestor_symlink.go`

**Safety Gates (evaluated in order):**

| Gate | Check | Denial Reason |
|------|-------|---------------|
| 1 | Ancestor Symlink Containment | `symlink_ancestor_escape` |
| 2 | Outside Allowed Roots | `outside_allowed_roots` |
| 3 | Protected Paths | `protected_path` |
| 4 | Symlink Escape | `symlink_escape` |
| 5 | Mount Boundary | `cross_device` |
| 6 | Directory Delete | `dir_delete_disabled` |
| 7 | Parent Accessible | `parent_inaccessible` |

**Protected Paths (default):**
- `/etc`, `/boot`, `/usr`, `/var`, `/sys`, `/proc`, `/dev`

**Design Decision:** Always uses `lstat` instead of `stat`. Using `stat` would follow symlinks, enabling escape attacks where a symlink points outside allowed roots.

---

### `internal/planner` — Plan Generation
**Files:** `planner.go`, `planner_test.go`, `planner_bench_test.go`

```go
func (p *Simple) BuildPlan(ctx, candidates, policy, safety, env, cfg) ([]PlanItem, error)
```

**Process:**
1. Consume all candidates from channel
2. For each candidate:
   - Evaluate policy → `Decision`
   - Evaluate safety → `SafetyVerdict`
   - Record metrics
3. Sort by path (deterministic ordering)
4. Calculate eligible files/bytes
5. Return complete plan slice

**Design Decision:** Buffers streaming candidates into slice for sorting. Deterministic ordering ensures reproducible results across runs.

---

### `internal/executor` — TOCTOU-Safe Deletion
**Files:** `simple.go`, `simple_test.go`

```go
func (e *Simple) Execute(ctx, planItem, mode) ActionResult
```

**Hard Gates (sequential, fail-closed):**

| Gate | Check | On Failure |
|------|-------|------------|
| 1 | `planItem.Decision.Allow` | Skip (policy denied) |
| 2 | `planItem.Safety.Allowed` | Skip (scan-time safety denied) |
| 3 | **Execute-time re-check** | Skip (TOCTOU protection) |
| 4 | Dry-run mode | Return `would_delete` |
| 5 | Trash enabled | Move to trash |
| 6 | Execute mode | Permanent deletion |

**Outcomes:**
- `deleted` — Permanently removed
- `trashed` — Moved to trash directory
- `would_delete` — Dry-run mode, no action taken
- `already_gone` — File disappeared between scan and execute
- `delete_failed` — OS error during deletion

**Design Decision:** TOCTOU (Time-of-Check-Time-of-Use) re-validation immediately before mutation closes the race condition window where files could be modified between scan and delete.

---

### `internal/auditor` — Audit Trail
**Files:** `jsonl.go`, `ndjson.go`, `sqlite.go`, `multi.go`, `*_test.go`

| Auditor | Storage | Features |
|---------|---------|----------|
| `JSONL` | Append-only file | Streaming, human-readable |
| `SQLite` | Database file | Queryable, checksummed, WAL mode |
| `Multi` | Delegates to both | Dual logging for redundancy |

**SQLite Schema:**
```sql
CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    level TEXT NOT NULL,
    action TEXT NOT NULL,
    path TEXT,
    mode TEXT,
    decision TEXT,
    reason TEXT,
    score INTEGER,
    bytes_freed INTEGER,
    error TEXT,
    fields TEXT,          -- JSON-encoded extra fields
    checksum TEXT NOT NULL -- SHA256 for tamper detection
);
```

**Key Methods:**
- `Record(ctx, event)` — Write audit entry
- `Query(ctx, filter)` — Search with filters
- `Stats(ctx)` — Aggregate statistics
- `VerifyIntegrity(ctx)` — Detect tampering via checksums

**Design Decision:** Per-row SHA256 checksums enable tamper detection for compliance/forensic requirements. Pure-Go SQLite (`modernc.org/sqlite`) avoids CGO for easier cross-compilation.

---

### `internal/daemon` — Long-Running Service
**Files:** `daemon.go`, `daemon_test.go`, `disk_unix.go`, `disk_windows.go`

**State Machine:**
```
Starting → Ready ⇄ Running → Stopping → Stopped
```

**HTTP Endpoints:**

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/health` | GET | Liveness probe (always 200 if alive) |
| `/ready` | GET | Readiness probe (503 if stopping or disk >95%) |
| `/status` | GET | State, run count, last run, errors |
| `/trigger` | POST | Manual cleanup run |
| `/api/config` | GET | Current configuration |
| `/api/audit/query` | GET | Query audit records |
| `/api/audit/stats` | GET | Audit statistics |
| `/api/trash` | GET/DELETE | List/empty trash |
| `/api/trash/restore` | POST | Restore from trash |
| `/api/scheduler/start` | POST | Enable scheduler |
| `/api/scheduler/stop` | POST | Disable scheduler |

**Disk-Aware Auto-Cleanup:**
- **90% usage:** Auto-purge old trash items before run
- **95% usage:** Bypass trash entirely (permanent delete)

**Design Decision:** Disk thresholds prevent disk-full scenarios from blocking cleanup. Graceful shutdown waits for in-flight runs to complete before closing resources.

---

### `internal/trash` — Soft-Delete
**Files:** `trash.go`, `trash_test.go`

```go
func (m *Manager) MoveToTrash(path string) (trashPath string, err error)
func (m *Manager) Restore(trashPath string) (originalPath string, err error)
func (m *Manager) List() ([]TrashItem, error)
func (m *Manager) Cleanup(ctx context.Context) (count int, bytesFreed int64, err error)
```

**Naming Convention:**
```
YYYYMMDD-HHMMSS_<hash>_<originalname>
YYYYMMDD-HHMMSS_<hash>_<originalname>.meta
```

**Metadata File (`.meta`):**
```yaml
original_path: /data/old_log.txt
trashed_at: 2024-01-15T10:30:00Z
size: 524288
mode: -rw-r--r--
mod_time: 2024-01-10T08:00:00Z
```

**Design Decision:** Sidecar metadata files enable restoration to original path and survive trash directory moves. Hash in filename prevents collisions.

---

### `internal/logger` — Structured Logging
**Files:** `logger.go`, `loki.go`, `*_test.go`

```go
type Logger interface {
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    WithFields(fields ...Field) Logger
}
```

**Loki Integration:**
- Async batching (configurable batch size/wait)
- Custom labels (service, environment)
- Tenant ID support for multi-tenant Loki
- Graceful flush on shutdown

**Design Decision:** Field builder pattern (`logger.F("key", value)`) for type-safe structured logging.

---

### `internal/metrics` — Prometheus Instrumentation
**Files:** `prometheus.go`, `noop.go`, `server.go`, `*_test.go`

| Metric | Type | Labels |
|--------|------|--------|
| `storagesage_scanner_files_scanned_total` | Counter | root |
| `storagesage_scanner_dirs_scanned_total` | Counter | root |
| `storagesage_scanner_scan_duration_seconds` | Histogram | root |
| `storagesage_planner_policy_decisions_total` | Counter | reason, allowed |
| `storagesage_planner_safety_verdicts_total` | Counter | reason, allowed |
| `storagesage_planner_files_eligible` | Gauge | — |
| `storagesage_planner_bytes_eligible` | Gauge | — |
| `storagesage_executor_files_deleted_total` | Counter | root |
| `storagesage_executor_dirs_deleted_total` | Counter | root |
| `storagesage_executor_bytes_freed_total` | Counter | — |
| `storagesage_executor_delete_errors_total` | Counter | reason |
| `storagesage_system_disk_usage_percent` | Gauge | — |
| `storagesage_daemon_last_run_timestamp_seconds` | Gauge | — |

**Design Decision:** Noop implementation allows disabling metrics without code changes. All metric operations are nil-safe.

---

### `internal/auth` — API Security
**Files:** `auth.go`, `apikey.go`, `middleware.go`, `rbac.go`, `*_test.go`

| Role | Permissions |
|------|-------------|
| `Viewer` | Read-only: status, audit, config |
| `Operator` | + Trigger runs, manage trash |
| `Admin` | + All operations |

**Context Propagation:**
```go
// Extract identity from request context
identity := auth.IdentityFromContext(ctx)

// Store identity in context
ctx = auth.ContextWithIdentity(ctx, identity)
```

**Design Decision:** Context-based identity propagation follows Go best practices. Pluggable authenticators enable future auth methods.

---

### `internal/notifier` — Webhook Notifications
**Files:** `webhook.go`, `webhook_test.go`

**Event Types:**
- `CleanupStarted`
- `CleanupCompleted`
- `CleanupFailed`
- `DaemonStarted`
- `DaemonStopped`

**Payload Structure:**
```json
{
  "event": "cleanup_completed",
  "timestamp": "2024-01-15T10:30:00Z",
  "message": "Cleanup completed successfully",
  "summary": {
    "root": "/tmp",
    "mode": "execute",
    "files_scanned": 1500,
    "files_deleted": 45,
    "bytes_freed": 104857600,
    "errors": 0,
    "duration": "5s"
  }
}
```

**Design Decision:** Fire-and-forget async delivery. Failed webhooks are logged but don't block operations.

---

### `internal/pidfile` — Single-Instance Enforcement
**Files:** `pidfile_unix.go`, `pidfile_windows.go`, `pidfile_test.go`

```go
func New(path string) (*PIDFile, error)  // Create and lock
func (p *PIDFile) Close() error          // Release lock
```

**Design Decision:** Platform-specific implementations for robust PID file handling across Unix and Windows.

---

### `internal/web` — Embedded Frontend
**Files:** `embed.go`

```go
//go:embed dist/*
var distFS embed.FS

func DistFS() (fs.FS, error)
func HasDist() bool
```

**Design Decision:** Go 1.16+ embed directive bundles the React build into the binary for single-file deployment.

---

## Frontend (web/src/)

### API Layer
**Files:** `api/types.ts`, `api/client.ts`

TypeScript types mirror Go structs for type safety.

### Pages

| Page | Purpose |
|------|---------|
| `Dashboard.tsx` | Status, trigger button, quick stats |
| `History.tsx` | Audit record query/filtering |
| `Config.tsx` | Configuration viewer |
| `Metrics.tsx` | Prometheus metrics display |
| `Trash.tsx` | Trash list/restore/empty |

### Hooks

| Hook | Purpose |
|------|---------|
| `useStatus()` | Poll `/status` endpoint |
| `useAuditHistory()` | Query audit records |
| `useTrash()` | Trash management |
| `useScheduler()` | Scheduler control |

---

## Entry Point (cmd/storage-sage/main.go)

### CLI Subcommands

| Command | Purpose |
|---------|---------|
| `init` | First-time setup (config, audit db, trash dir) |
| `query` | Query audit database |
| `stats` | Audit statistics |
| `verify` | Check audit integrity (checksums) |
| `validate` | Validate config file |
| `trash list` | List trash contents |
| `trash restore` | Restore from trash |
| `trash empty` | Empty trash |

### Execution Modes

**Daemon Mode** (`-daemon`):
- Long-running with HTTP API
- Scheduled cleanup via cron-like scheduler
- Persists across runs

**One-Shot Mode** (default):
- Single cleanup cycle
- Exits after completion

### Core Pipeline (`runCore()`):
1. Load config + merge CLI flags
2. Initialize logger + metrics
3. Initialize auditors (JSONL, SQLite)
4. Build policy from config
5. Scan filesystem → stream candidates
6. Plan generation (policy + safety evaluation)
7. Sort plan (allowed+safe first, then by score)
8. Log audit events for all plan items
9. If execute mode: run deletions, record results
10. Print summary

---

## Data Flow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Scanner   │────▶│   Planner   │────▶│  Executor   │
│             │     │             │     │             │
│ Candidates  │     │ Policy +    │     │ TOCTOU +    │
│ (streaming) │     │ Safety eval │     │ Delete/Trash│
└─────────────┘     └─────────────┘     └─────────────┘
       │                   │                   │
       ▼                   ▼                   ▼
┌─────────────────────────────────────────────────────┐
│                      Auditor                        │
│         (JSONL + SQLite with checksums)             │
└─────────────────────────────────────────────────────┘
       │
       ▼
┌─────────────────────────────────────────────────────┐
│                      Metrics                        │
│              (Prometheus counters/gauges)           │
└─────────────────────────────────────────────────────┘
```

---

## Dependency Graph

```
cmd/storage-sage/main.go
├── internal/auditor
├── internal/auth
├── internal/config
├── internal/core
├── internal/daemon
├── internal/executor
├── internal/logger
├── internal/metrics
├── internal/notifier
├── internal/planner
├── internal/policy
├── internal/safety
├── internal/scanner
├── internal/trash
└── internal/web

internal/daemon
├── internal/auditor
├── internal/auth
├── internal/config
├── internal/logger
├── internal/pidfile
├── internal/trash
└── internal/web

internal/executor
├── internal/core
├── internal/daemon (for BypassTrashFromContext)
├── internal/logger
├── internal/metrics
├── internal/safety
└── internal/trash

internal/scanner
├── internal/core
├── internal/logger
└── internal/metrics

internal/planner
├── internal/core
├── internal/logger
└── internal/metrics

internal/safety
├── internal/core
└── internal/logger

internal/policy
└── internal/core

internal/auditor
└── internal/core

internal/auth
└── internal/logger

internal/metrics
└── internal/core

internal/trash
└── internal/logger
```

---

## External Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/prometheus/client_golang` | Prometheus metrics |
| `modernc.org/sqlite` | Pure-Go SQLite (no CGO) |
| `gopkg.in/yaml.v3` | Config parsing |
| `golang.org/x/sys` | Platform-specific syscalls |

**Design Decision:** Pure-Go SQLite (`modernc.org/sqlite`) avoids CGO dependency, enabling easier cross-compilation and fully static binaries.

---

## Key Architectural Decisions Summary

| Decision | Rationale |
|----------|-----------|
| **Interface-first in `core/`** | Enables mocking, testing, and swappable implementations |
| **Channel-based scanning** | Non-blocking; handles large directories without memory pressure |
| **TOCTOU re-validation** | Closes race condition window between scan and delete |
| **Composite policy pattern** | Flexible AND/OR combination without code changes |
| **Checksummed audit records** | Forensic integrity for compliance/government use |
| **Soft-delete default** | Recovery possible; permanent deletion requires explicit config |
| **Disk-aware auto-cleanup** | Self-healing under disk pressure (90%/95% thresholds) |
| **Embedded web UI** | Single binary deployment; no external dependencies |
| **lstat over stat** | Security: never follow symlinks to prevent escape attacks |
| **Fail-soft scanning** | Skip inaccessible paths instead of aborting entire scan |
| **Pure-Go SQLite** | No CGO for easier cross-compilation |

---

## Safety Guarantees

1. **No Deletion Without Decision** — Plan-time policy + safety evaluation required
2. **No TOCTOU Races** — Execute-time safety re-check immediately before mutation
3. **No Cross-Device** — Mount boundary enforcement (optional)
4. **No Symlink Escapes** — Ancestor & target symlink containment checked
5. **No Protected Paths** — System-critical paths always blocked
6. **No Silent Directory Deletion** — Requires explicit `allow_dir_delete`
7. **Audit Immutability** — Write-once records with tamper detection checksums
8. **Recoverable Deletion** — Soft-delete to trash with full restoration support

---

## Quick Reference: Adding New Features

### Adding a New Policy
1. Create `internal/policy/newpolicy.go`
2. Implement `core.Policy` interface (`Evaluate(ctx, candidate) Decision`)
3. Register in `cmd/storage-sage/main.go` policy chain

### Adding a New API Endpoint
1. Add handler in `internal/daemon/daemon.go`
2. Register route in `startHTTP()`
3. Add TypeScript types in `web/src/api/types.ts`
4. Create React hook in `web/src/hooks/`

### Adding a New Metric
1. Add field to `internal/metrics/prometheus.go`
2. Register in `NewPrometheus()`
3. Add method to `core.Metrics` interface
4. Implement in both `Prometheus` and `Noop`

### Adding a New Audit Field
1. Add to `Fields` map in `core.AuditEvent`
2. Extract in `internal/auditor/sqlite.go` `Record()` method
3. Add column if needed (requires migration)
