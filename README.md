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
| `-exclude` | | Comma-separated glob patterns to exclude (e.g., `*.important,keep-*`) |
| `-depth` | `0` | Max traversal depth (0 = unlimited) |
| `-max` | `25` | Max plan items to display in output |
| `-protected` | | Additional protected paths (comma-separated) |
| `-allow-dir-delete` | `false` | Allow deletion of directories |
| `-audit` | | Path to JSONL audit log (empty = disabled) |
| `-audit-db` | | Path to SQLite audit database (for long-term storage) |
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

### Exclusion Policy (Optional)

When `-exclude` is set, files matching any exclusion pattern are **never deleted**, regardless of other policy matches. Patterns use glob syntax:

```bash
# Exclude specific extensions
storage-sage -root /tmp -exclude "*.important,*.keep"

# Exclude files with prefix
storage-sage -root /data -exclude "keep-*,backup-*"

# Exclude entire directories (recursive)
storage-sage -root /project -exclude "node_modules/**,.git/**"
```

**Supported patterns:**
- `*` - Match any characters in filename (e.g., `*.bak`, `keep-*`)
- `?` - Match single character (e.g., `log?.txt`)
- `[...]` - Character class (e.g., `[0-9]*.log`)
- `dir/**` - Match all files under directory recursively

### How Policies Combine

Policies combine with **AND** logic:

```
Eligible = Age OK AND (Size OK OR Extension OK) AND NOT Excluded
```

A file must always be old enough, AND if you've specified size or extension filters, it must match at least one of those too. Files matching any exclusion pattern are always protected.

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

## SQLite Audit Database

For long-term audit retention and offline operation (ideal for government/rugged systems), use the SQLite audit backend with `-audit-db`:

```bash
# Enable SQLite audit logging
storage-sage -root /tmp -mode execute -audit-db /var/lib/storage-sage/audit.db

# Use both JSONL and SQLite simultaneously
storage-sage -root /tmp -audit /var/log/storage-sage.jsonl -audit-db /var/lib/storage-sage/audit.db
```

### Why SQLite for Government/Rugged Systems

- **Offline operation**: No external database server required
- **Single file**: Easy backup, transport, and archival
- **Tamper detection**: SHA256 checksums on every record
- **Long-term retention**: Query logs from months/years ago
- **No dependencies**: Pure Go implementation, no CGO required

### Query Audit Logs

```bash
# View recent logs
storage-sage query -db audit.db -since 24h

# Filter by action
storage-sage query -db audit.db -action delete

# Filter by error level
storage-sage query -db audit.db -level error

# Export as JSON
storage-sage query -db audit.db -since 7d -json > report.json
```

### View Statistics

```bash
storage-sage stats -db audit.db
# Output:
# Audit Database Statistics
# =========================
# Total Records:     15432
# First Record:      2024-01-01 00:00:00
# Last Record:       2024-06-15 14:30:00
# Files Deleted:     8921
# Total Bytes Freed: 1.2 TB
# Errors:            12
```

### Verify Integrity

Detect any tampering with historical audit records:

```bash
storage-sage verify -db audit.db
# PASS: All records verified. No tampering detected.
```

### Configuration File

```yaml
execution:
  mode: execute
  audit_path: /var/log/storage-sage.jsonl    # JSONL (optional)
  audit_db_path: /var/lib/storage-sage/audit.db  # SQLite (recommended)
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

policy:
  min_age_days: 30
  min_size_mb: 0
  extensions: []
  exclusions:
    - "*.important"
    - "keep-*"
    - ".git/**"

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

## Loki Log Aggregation

Storage-Sage can ship logs to Grafana Loki for centralized log aggregation. Logs are sent to both the console/file output AND Loki asynchronously.

### Enabling Loki

```bash
# Basic usage - ship logs to local Loki
storage-sage -root /tmp -loki -loki-url http://localhost:3100

# With daemon mode
storage-sage -daemon -schedule 1h -root /tmp -loki -loki-url http://loki.example.com:3100
```

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-loki` | `false` | Enable Loki log shipping |
| `-loki-url` | `http://localhost:3100` | Loki server URL |

### Configuration File

Loki settings can be specified in the YAML configuration file for more control:

```yaml
logging:
  level: info
  format: json
  loki:
    enabled: true
    url: "http://localhost:3100"
    batch_size: 100           # Number of entries before flush
    batch_wait: 5s            # Max time before flush
    tenant_id: "my-tenant"    # X-Scope-OrgID header (optional)
    labels:
      service: storage-sage
      environment: production
```

### How It Works

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────┐
│  Application    │────▶│   LokiLogger    │────▶│    Loki     │
│                 │     │   (decorator)   │     │   Server    │
│  log.Info(...)  │     │                 │     │             │
└─────────────────┘     │  ┌───────────┐  │     └─────────────┘
                        │  │ Base Log  │──┼────▶ Console/File
                        │  └───────────┘  │
                        │  ┌───────────┐  │
                        │  │  Batcher  │──┼────▶ HTTP POST
                        │  └───────────┘  │     /loki/api/v1/push
                        └─────────────────┘
```

- **Non-blocking**: Logs are batched and sent asynchronously
- **Dual output**: Console/file logging continues to work normally
- **Batching**: Logs are batched by count (`batch_size`) or time (`batch_wait`)
- **Graceful shutdown**: Pending logs are flushed on exit

### Querying Logs

```bash
# Query logs via Loki API
curl -G -s "http://localhost:3100/loki/api/v1/query" \
  --data-urlencode 'query={service="storage-sage"}'

# Filter by log level
curl -G -s "http://localhost:3100/loki/api/v1/query" \
  --data-urlencode 'query={service="storage-sage",level="error"}'
```

### Running Loki Locally

```bash
# Start Loki with Docker
docker run -d --name loki -p 3100:3100 grafana/loki:latest

# Test with storage-sage
storage-sage -root /tmp -loki -loki-url http://localhost:3100
```

## Webhook Notifications

Storage-Sage can send notifications to HTTP endpoints when cleanup events occur. This is useful for alerting via Slack, Discord, PagerDuty, or custom systems.

### Configuration

Webhooks are configured in the YAML configuration file:

```yaml
notifications:
  webhooks:
    # Generic webhook
    - url: "https://your-server.com/webhook"
      headers:
        Authorization: "Bearer your-token"
      events:
        - cleanup_completed
        - cleanup_failed

    # Slack webhook
    - url: "https://hooks.slack.com/services/T00/B00/XXX"
      events:
        - cleanup_completed
        - cleanup_failed
```

### Event Types

| Event | Description |
|-------|-------------|
| `cleanup_started` | Cleanup run has begun |
| `cleanup_completed` | Cleanup finished successfully |
| `cleanup_failed` | Cleanup encountered an error |

### Webhook Payload

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
    "duration": "5s",
    "started_at": "2024-01-15T10:29:55Z",
    "completed_at": "2024-01-15T10:30:00Z"
  }
}
```

### Slack Integration

For Slack, use an incoming webhook URL. The payload is JSON-formatted and can be parsed by Slack workflows or custom handlers.

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
    loki.go            # Loki log shipping
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
