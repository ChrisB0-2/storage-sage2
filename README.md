# Storage-Sage

A safety-first file cleanup tool that intelligently identifies and removes old files from disk storage.

Storage-Sage is designed for system administrators and automated cleanup operations where **accidental data loss must be prevented at all costs**. Rather than being a simple deletion tool, it's an elaborate safety system with multiple layers of protection.

## Why Storage-Sage?

Most cleanup tools are "delete first, regret later." Storage-Sage inverts this:

- **Fail-closed design** - When in doubt, don't delete
- **Multiple independent safety gates** - No single point of failure
- **TOCTOU protection** - Guards against race condition attacks
- **Complete audit trail** - Every decision is logged with reasoning
- **Dry-run by default** - You must explicitly opt into deletion

## Features

- **Multi-layered safety**: Policy checks, safety validation, and TOCTOU (time-of-check-time-of-use) protection
- **Symlink attack prevention**: Detects and blocks symlink escape attempts
- **Protected paths**: System directories protected by default
- **Flexible policies**: Age, size, and extension-based filtering with composable rules
- **Audit logging**: JSONL audit trail of all decisions and actions
- **Dry-run mode**: Preview what would be deleted before executing
- **Daemon mode**: Run as a long-running service with scheduled cleanup
- **Prometheus metrics**: Built-in metrics endpoint for monitoring
- **Zero dependencies**: Pure Go standard library for maximum reliability (plus optional Prometheus)

## Installation

```bash
go install github.com/ChrisB0-2/storage-sage/cmd/storage-sage@latest
```

Or build from source:

```bash
git clone https://github.com/ChrisB0-2/storage-sage.git
cd storage-sage
go build ./cmd/storage-sage
```

## Quick Start

### Preview what would be deleted (dry-run)

```bash
# Find files older than 30 days in /tmp
storage-sage -root /tmp

# Find files older than 7 days and larger than 100MB
storage-sage -root /data/logs -min-age-days 7 -min-size-mb 100

# Find old .tmp and .log files
storage-sage -root /var/cache -min-age-days 14 -extensions ".tmp,.log"
```

### Actually delete files (execute mode)

```bash
# Delete files older than 30 days in /tmp (with audit log)
storage-sage -root /tmp -mode execute -audit /var/log/storage-sage.jsonl
```

### Delete directories too

```bash
# Include empty directories in cleanup
storage-sage -root /data/temp -mode execute -allow-dir-delete
```

## Safety Architecture

Storage-Sage implements **four independent safety gates**. A file must pass ALL of them to be deleted:

```
                                    ┌─────────────────────┐
                                    │   File Candidate    │
                                    └──────────┬──────────┘
                                               │
                         ┌─────────────────────▼─────────────────────┐
                         │            GATE 1: Policy                 │
                         │  Age threshold, size filter, extensions   │
                         │         (configurable rules)              │
                         └─────────────────────┬─────────────────────┘
                                               │ Pass
                         ┌─────────────────────▼─────────────────────┐
                         │       GATE 2: Scan-Time Safety            │
                         │   Protected paths, allowed roots check,   │
                         │   symlink escape detection                │
                         └─────────────────────┬─────────────────────┘
                                               │ Pass
                         ┌─────────────────────▼─────────────────────┐
                         │      GATE 3: Ancestor Symlink Check       │
                         │  Validates no symlink in path ancestry    │
                         │  escapes the allowed root (lstat-based)   │
                         └─────────────────────┬─────────────────────┘
                                               │ Pass
                         ┌─────────────────────▼─────────────────────┐
                         │     GATE 4: Execute-Time Re-Check         │
                         │   TOCTOU protection - re-validates all    │
                         │   safety checks immediately before delete │
                         └─────────────────────┬─────────────────────┘
                                               │ Pass
                                    ┌──────────▼──────────┐
                                    │   File Deleted      │
                                    │   (audit logged)    │
                                    └─────────────────────┘
```

### Protected Paths (Default)

The following system directories are protected by default and **cannot** be deleted:

- `/boot`, `/etc`, `/usr`, `/var`
- `/sys`, `/proc`, `/dev`

Add additional protected paths with `-protected /path1,/path2`.

### Symlink Protection

Storage-Sage uses `lstat` (not `stat`) to analyze paths without following symlinks. It detects:

- **symlink_self**: The file itself is a symlink
- **symlink_ancestor**: A directory in the path is a symlink
- **symlink_escape**: A symlink points outside allowed roots

### TOCTOU Protection

Time-of-check-time-of-use attacks are prevented by re-running all safety checks **immediately before deletion**. If a file changes between scan and execute, deletion is blocked.

## CLI Reference

| Flag | Default | Description |
|------|---------|-------------|
| `-root` | (required) | Root directory to scan |
| `-mode` | `dry-run` | Mode: `dry-run` (preview) or `execute` (delete) |
| `-min-age-days` | `30` | Minimum file age in days to consider for cleanup |
| `-min-size-mb` | `0` | Minimum file size in MB (0 = disabled) |
| `-extensions` | | Comma-separated extensions to match (e.g., `.tmp,.log`) |
| `-depth` | `0` | Max traversal depth (0 = unlimited) |
| `-max` | `25` | Max plan items to display in output |
| `-protected` | | Additional protected paths (comma-separated) |
| `-allow-dir-delete` | `false` | Allow deletion of directories |
| `-audit` | | Path to JSONL audit log (empty = disabled) |
| `-metrics` | `false` | Enable Prometheus metrics endpoint |
| `-metrics-addr` | `:9090` | Prometheus metrics server address |
| `-daemon` | `false` | Run as long-running daemon |
| `-schedule` | | Cleanup schedule (e.g., `1h`, `30m`, `@every 6h`) |
| `-daemon-addr` | `:8080` | Daemon HTTP endpoint address |

## Policy System

Storage-Sage uses a **composable policy system** to determine which files are candidates for deletion.

### Age Policy (Always Active)

Files must be older than `-min-age-days` to be considered. This is the primary filter.

### Size Policy (Optional)

When `-min-size-mb` is set, files must also meet the size threshold.

### Extension Policy (Optional)

When `-extensions` is set, files must have one of the specified extensions.

### How Policies Combine

Policies combine with **AND** logic:

```
Eligible = Age OK AND (Size OK OR Extension OK)
```

A file must always be old enough, AND if you've specified size or extension filters, it must match at least one of those too.

## Scoring System

Each eligible file receives a **score** that determines deletion priority:

```
Score = (age_in_days × 10) + size_in_MB
```

- **Age dominates**: A 100-day-old 1MB file (score: 1001) ranks higher than a 30-day-old 500MB file (score: 800)
- **Deterministic**: Same inputs always produce same ordering
- **Tiebreakers**: Size → modification time → path (for stable sorting)

Higher scores appear first in the plan and are deleted first in execute mode.

## Output Example

```
StorageSage (DRY PIPELINE)
root: /data/temp
candidates: 1547
policy allowed: 234
safety allowed: 198
eligible bytes (policy+safe): 15728640000
safety blocked: 1349
safety reasons:
  - outside_allowed_roots: 12
  - protected_path: 3
  - too_new: 1334

First 25 plan items:
- /data/temp/old_backup.tar.gz | score=892 | policy=age_ok | safety=allowed
- /data/temp/cache/stale.dat | score=445 | policy=age_ok | safety=allowed
...
```

## Audit Log Format

When `-audit` is specified, all decisions are logged in JSONL format:

```json
{"time":"2024-01-15T10:30:00Z","level":"info","action":"plan","path":"/data/old.log","fields":{"decision":"allow","reason":"age_ok","score":450}}
{"time":"2024-01-15T10:30:01Z","level":"info","action":"delete","path":"/data/old.log","fields":{"bytes_freed":1024,"reason":"deleted"}}
```

## Daemon Mode

Storage-Sage can run as a long-running daemon that performs scheduled cleanup operations. This is ideal for continuous maintenance of temporary directories, log files, and cache cleanup.

### Starting Daemon Mode

```bash
# Run cleanup every hour
storage-sage -daemon -schedule 1h -root /tmp -mode execute

# Run cleanup every 6 hours with audit logging
storage-sage -daemon -schedule 6h -root /data/cache -mode execute -audit /var/log/storage-sage.jsonl

# Custom HTTP endpoint address
storage-sage -daemon -schedule 30m -root /tmp -daemon-addr :9000
```

### Schedule Format

The `-schedule` flag accepts Go duration format:

- `1h` - Every hour
- `30m` - Every 30 minutes
- `6h` - Every 6 hours
- `1h30m` - Every 1 hour 30 minutes
- `@every 1h` - Alternative syntax (same as `1h`)

### HTTP API

The daemon exposes HTTP endpoints for monitoring and control:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Liveness check (always returns 200) |
| `/ready` | GET | Readiness check (200 if ready/running, 503 otherwise) |
| `/status` | GET | Detailed status with last run info, run count, schedule |
| `/trigger` | POST | Manually trigger a cleanup run |

### Example API Usage

```bash
# Check if daemon is healthy
curl http://localhost:8080/health
# {"status":"ok","state":"ready"}

# Check if daemon is ready to accept work
curl http://localhost:8080/ready
# {"ready":true,"state":"ready"}

# Get detailed status
curl http://localhost:8080/status
# {"state":"ready","running":false,"last_run":"2024-01-15T10:30:00Z","last_error":"","run_count":5,"schedule":"1h"}

# Manually trigger a cleanup
curl -X POST http://localhost:8080/trigger
# {"triggered":true}
```

### Configuration File

Daemon settings can also be specified in the YAML configuration file:

```yaml
daemon:
  enabled: true
  http_addr: ":8080"
  metrics_addr: ":9090"
  schedule: "6h"

scan:
  roots:
    - /tmp
    - /var/cache

execution:
  mode: execute
  audit_path: /var/log/storage-sage.jsonl
```

### Graceful Shutdown

The daemon handles `SIGINT` and `SIGTERM` signals for graceful shutdown:

- Stops accepting new scheduled runs
- Waits for any in-progress cleanup to complete
- Shuts down HTTP server cleanly
- Exits with code 0

### Running as a System Service

Example systemd unit file (`/etc/systemd/system/storage-sage.service`):

```ini
[Unit]
Description=Storage-Sage Cleanup Daemon
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/storage-sage -daemon -schedule 6h -root /tmp -mode execute
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

## Architecture

```
cmd/storage-sage/
  main.go              # Entry point, CLI parsing, orchestration

internal/
  core/
    contracts.go       # Interfaces and types (Scanner, Policy, Safety, etc.)
    audit_helpers.go   # Audit event builders

  scanner/
    walkdir.go         # Filesystem traversal

  policy/
    age.go             # Age-based policy
    size.go            # Size-based policy
    extension.go       # Extension-based policy
    composite.go       # AND/OR policy composition

  safety/
    safety.go          # Path validation (protected paths, allowed roots)
    ancestor_symlink.go # Symlink containment checks

  planner/
    planner.go         # Combines candidates with policy + safety decisions

  executor/
    simple.go          # Safe deletion with TOCTOU re-check

  auditor/
    jsonl.go           # JSONL audit logger
    ndjson.go          # NDJSON audit logger

  daemon/
    daemon.go          # Long-running daemon with scheduling and HTTP API

  metrics/
    prometheus.go      # Prometheus metrics collection
    server.go          # Metrics HTTP server

  config/
    config.go          # YAML configuration loading
    validate.go        # Configuration validation

  logger/
    logger.go          # Structured JSON logging
```

## Use Cases

- **Temporary file cleanup**: Remove old files from `/tmp`, cache directories
- **Log rotation**: Delete logs older than retention period
- **Build artifact cleanup**: Remove old `.o`, `.pyc`, compiled files
- **Backup pruning**: Delete backups older than N days
- **CI/CD cleanup**: Prune old test artifacts and intermediate files

## Best Practices

1. **Always dry-run first**: Never run `-mode execute` without previewing
2. **Use audit logs**: Enable `-audit` for compliance and debugging
3. **Start conservative**: Begin with longer age thresholds, tighten over time
4. **Protect important paths**: Use `-protected` for application-specific directories
5. **Monitor the output**: Review safety block reasons to understand what's being protected

## Running Tests

```bash
go test ./...
```

## License

MIT
