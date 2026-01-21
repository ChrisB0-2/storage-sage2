# Storage-Sage User Manual

A complete guide to installing, configuring, and running Storage-Sage for automated file cleanup with full observability.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Installation](#installation)
3. [Quick Start (CLI)](#quick-start-cli)
4. [Running the Full Stack](#running-the-full-stack)
5. [Web UI Guide](#web-ui-guide)
6. [Grafana Dashboard](#grafana-dashboard)
7. [Configuration Reference](#configuration-reference)
8. [Troubleshooting](#troubleshooting)

---

## Prerequisites

### For CLI Usage
- Go 1.21 or later

### For Full Stack (Daemon + Observability)
- Podman or Docker
- podman-compose or docker-compose

**Installing podman-compose (RHEL/Fedora):**
```bash
pip install podman-compose
```

---

## Installation

### Option 1: Build from Source
```bash
git clone https://github.com/ChrisB0-2/storage-sage.git
cd storage-sage
go build ./cmd/storage-sage
```

### Option 2: Go Install
```bash
go install github.com/ChrisB0-2/storage-sage/cmd/storage-sage@latest
```

### Verify Installation
```bash
./storage-sage --help
```

---

## Quick Start (CLI)

### Preview Files (Dry-Run Mode)

By default, Storage-Sage runs in dry-run mode - it shows what would be deleted without actually deleting anything.

```bash
# Find files older than 30 days in /tmp
./storage-sage -root /tmp

# Find files older than 7 days
./storage-sage -root /data/logs -min-age-days 7

# Find files larger than 100MB and older than 14 days
./storage-sage -root /data -min-age-days 14 -min-size-mb 100

# Find specific file types
./storage-sage -root /var/cache -extensions ".tmp,.log,.bak"
```

### Actually Delete Files (Execute Mode)

**WARNING:** This permanently deletes files. Always dry-run first!

```bash
# Delete old files with audit logging
./storage-sage -root /tmp -mode execute -audit /var/log/storage-sage.jsonl

# Delete with SQLite audit database (recommended)
./storage-sage -root /tmp -mode execute -audit-db /var/lib/storage-sage/audit.db
```

### Common CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-root` | (required) | Directory to scan |
| `-mode` | `dry-run` | `dry-run` (preview) or `execute` (delete) |
| `-min-age-days` | `30` | Minimum file age to consider |
| `-min-size-mb` | `0` | Minimum file size (0 = disabled) |
| `-extensions` | | File extensions to match (e.g., `.tmp,.log`) |
| `-exclude` | | Patterns to exclude (e.g., `*.important,keep-*`) |
| `-audit` | | Path to JSONL audit log |
| `-audit-db` | | Path to SQLite audit database |

---

## Running the Full Stack

The full stack includes:
- **Storage-Sage Daemon** - Scheduled cleanup service with web UI
- **Prometheus** - Metrics collection
- **Grafana** - Visualization and dashboards
- **Loki** - Log aggregation

### Step 1: Prepare the Data Directory

Create a directory for files to be cleaned:
```bash
cd storage-sage
mkdir -p data
```

### Step 2: Start the Stack

```bash
podman-compose up -d
```

Or with Docker:
```bash
docker-compose up -d
```

### Step 3: Verify Services

```bash
podman ps
```

You should see 4 containers running:
- `storage-sage` - Port 8080 (Web UI + API), Port 9091 (Metrics)
- `prometheus` - Port 9090
- `grafana` - Port 3000
- `loki` - Port 3100

### Step 4: Access the Services

| Service | URL | Credentials |
|---------|-----|-------------|
| Storage-Sage Web UI | http://localhost:8080 | None |
| Grafana | http://localhost:3000 | admin / changeme |
| Prometheus | http://localhost:9090 | None |

### Step 5: Stopping the Stack

```bash
podman-compose down
```

To also remove data volumes (fresh start):
```bash
podman-compose down
podman volume rm storage-sage_prometheus_data storage-sage_grafana_data storage-sage_storage_sage_data
```

---

## Web UI Guide

Access the web UI at **http://localhost:8080**

### Dashboard Page

The main dashboard shows:
- **Daemon Status** - Current state (ready/running)
- **Trigger Button** - Manually start a cleanup run
- **Quick Stats** - Total records, files deleted, space freed, errors

### Metrics Page

Detailed metrics including:
- All-time statistics
- Last 7 days activity
- Event level distribution (Info/Warning/Error)
- Event action distribution (Plan/Execute)
- **Open in Grafana** button - Direct link to full Grafana dashboard

### History Page

Browse the complete audit log:
- Filter by date range
- Filter by action type
- Filter by log level
- View detailed information for each event

### Config Page

View the current daemon configuration.

---

## Grafana Dashboard

### Accessing Grafana

1. Click **"Open in Grafana"** button in the Storage-Sage Metrics page
2. Or navigate directly to http://localhost:3000
3. Login with **admin / changeme**

### Dashboard Panels

The Storage-Sage Operations dashboard includes:

**Overview Section:**
- Total Files Scanned
- Files Eligible for Deletion
- Files Deleted
- Bytes Freed
- Bytes Eligible
- Delete Errors

**Scanning Section:**
- Files Scanned Rate (over time)
- Scan Duration (p50, p95 percentiles)

**Policy & Safety Section:**
- Policy Decisions (pie chart)
- Safety Verdicts (pie chart)
- Safety Blocks Over Time

**Execution Section:**
- Files Deleted Rate
- Bytes Freed Rate
- Delete Errors by Reason

### Viewing Logs with Loki

Storage-Sage ships logs to Loki for centralized log aggregation.

**Access logs in Grafana:**
1. Go to http://localhost:3000
2. Click **Explore** (compass icon in left sidebar)
3. Select **Loki** from the datasource dropdown
4. Enter a query and click **Run query**

**Common Loki Queries:**
```
# All storage-sage logs
{service="storage-sage"}

# Only error logs
{service="storage-sage"} |= "error"

# Only delete actions
{service="storage-sage"} |= "delete"

# Filter by log level
{service="storage-sage",level="warn"}
```

**Query logs via CLI:**
```bash
# Query recent logs
curl -s "http://localhost:3100/loki/api/v1/query?query={service=\"storage-sage\"}" | python3 -m json.tool
```

### Changing Time Range

Use the time picker in the top-right corner to adjust the view:
- Last 5 minutes (real-time)
- Last 1 hour (default)
- Last 24 hours
- Custom range

---

## Configuration Reference

### Docker Compose Configuration

The `docker-compose.yml` controls the full stack. Key settings:

```yaml
storage-sage:
  command:
    - "-daemon"              # Run as daemon
    - "-schedule=1m"         # Cleanup every minute (use 1h for production)
    - "-root=/data"          # Directory to clean
    - "-mode=execute"        # execute or dry-run
    - "-metrics"             # Enable Prometheus metrics
    - "-loki"                # Enable Loki logging
    - "-audit-db=/var/lib/storage-sage/audit.db"  # Audit database
```

### Changing the Schedule

Edit `docker-compose.yml` and change the schedule:
```yaml
- "-schedule=1h"    # Every hour
- "-schedule=6h"    # Every 6 hours
- "-schedule=30m"   # Every 30 minutes
```

Then restart:
```bash
podman-compose down
podman-compose up -d
```

### Changing Cleanup Rules

Add policy flags to the command:
```yaml
command:
  - "-daemon"
  - "-schedule=1h"
  - "-root=/data"
  - "-mode=execute"
  - "-min-age-days=7"        # Delete files older than 7 days
  - "-min-size-mb=10"        # Only files larger than 10MB
  - "-extensions=.tmp,.log"  # Only these extensions
```

### Using a Config File

Create `storage-sage.yaml`:
```yaml
scan:
  roots:
    - /data
    - /tmp

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
  audit_db_path: /var/lib/storage-sage/audit.db

daemon:
  schedule: "6h"

logging:
  level: info
  loki:
    enabled: true
    url: "http://loki:3100"

metrics:
  enabled: true
```

Mount it in `docker-compose.yml`:
```yaml
volumes:
  - ./storage-sage.yaml:/etc/storage-sage/config.yaml:ro,z
command:
  - "-daemon"
  - "-config=/etc/storage-sage/config.yaml"
```

---

## Troubleshooting

### Permission Denied Errors

If the daemon can't delete files:
```bash
# Fix permissions on the data directory
chmod -R 777 ./data
```

### SELinux Issues (RHEL/Fedora)

Volume mounts need the `:z` suffix for SELinux:
```yaml
volumes:
  - ./data:/data:rw,z
```

### Container Can't Start (cgroup errors)

Remove resource limits from `docker-compose.yml` if running rootless podman:
```yaml
# Remove or comment out these sections:
# deploy:
#   resources:
#     limits:
#       cpus: '0.5'
#       memory: 256M
```

### Grafana Shows Old/Stale Data

Reset Prometheus and Grafana volumes:
```bash
podman-compose down
podman volume rm storage-sage_prometheus_data storage-sage_grafana_data
podman-compose up -d
```

### Dashboard Not Found in Grafana

Ensure the dashboards volume is mounted with SELinux label:
```yaml
volumes:
  - ./dashboards:/var/lib/grafana/dashboards:ro,z
```

### Checking Logs

```bash
# Storage-sage logs
podman logs storage-sage

# Prometheus logs
podman logs prometheus

# Grafana logs
podman logs grafana

# All logs
podman-compose logs -f
```

### API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Liveness check |
| `/ready` | GET | Readiness check |
| `/status` | GET | Daemon status |
| `/trigger` | POST | Manually trigger cleanup |
| `/api/audit/stats` | GET | Audit statistics |
| `/api/audit/query` | GET | Query audit logs |
| `/api/config` | GET | Current configuration |

Example:
```bash
# Check status
curl http://localhost:8080/status

# Trigger cleanup
curl -X POST http://localhost:8080/trigger

# Get stats
curl http://localhost:8080/api/audit/stats
```

---

## Testing with Sample Files

Create test files to watch Storage-Sage in action.

### Create Old Files (Will Be Deleted)

```bash
cd /path/to/storage-sage/data

# Create 10 old .tmp files (60 days old)
for i in {1..10}; do
  echo "Old test file $i - sample content" > "old_file_$i.tmp"
  touch -d "60 days ago" "old_file_$i.tmp"
done

# Create 5 larger old .log files (45 days old, ~135KB each)
for i in {1..5}; do
  dd if=/dev/urandom bs=1024 count=100 2>/dev/null | base64 > "old_log_$i.log"
  touch -d "45 days ago" "old_log_$i.log"
done
```

### Create Recent Files (Will Be Protected)

```bash
# Create 3 recent files (these should NOT be deleted)
for i in {1..3}; do
  echo "Recent file $i" > "recent_$i.txt"
done
```

### Create Random Test Files

```bash
# Create 20 random files with varying ages and sizes
for i in {1..20}; do
  dd if=/dev/urandom bs=1024 count=$((RANDOM % 200 + 50)) 2>/dev/null | base64 > "testfile_$i.tmp"
  touch -d "$((RANDOM % 60 + 31)) days ago" "testfile_$i.tmp"
done
```

### Verify Test Files

```bash
ls -la
```

You should see files with various dates - old files from 30+ days ago and recent files from today.

### Trigger Cleanup and Watch

```bash
# Trigger a cleanup run
curl -X POST http://localhost:8080/trigger

# Watch the logs in real-time
podman logs -f storage-sage
```

### Verify Results

```bash
# Check remaining files (only recent files should remain)
ls -la

# Check audit stats
curl http://localhost:8080/api/audit/stats

# Check Grafana dashboard
# http://localhost:3000
```

### Complete Test Script

Save this as `create-test-files.sh`:

```bash
#!/bin/bash
# Create test files for Storage-Sage demo

DATA_DIR="${1:-./data}"
mkdir -p "$DATA_DIR"
cd "$DATA_DIR"

echo "Creating old files (will be deleted)..."
for i in {1..10}; do
  echo "Old test file $i - sample content for testing" > "old_file_$i.tmp"
  touch -d "60 days ago" "old_file_$i.tmp"
done

for i in {1..5}; do
  dd if=/dev/urandom bs=1024 count=100 2>/dev/null | base64 > "old_log_$i.log"
  touch -d "45 days ago" "old_log_$i.log"
done

echo "Creating recent files (will be protected)..."
for i in {1..3}; do
  echo "Recent file $i - should not be deleted" > "recent_$i.txt"
done

echo ""
echo "Created files:"
ls -la

echo ""
echo "Trigger cleanup with: curl -X POST http://localhost:8080/trigger"
```

Run it:
```bash
chmod +x create-test-files.sh
./create-test-files.sh ./data
```

---

## Safety Features

Storage-Sage is designed with safety as the top priority:

1. **Dry-run by default** - Must explicitly enable execute mode
2. **Protected paths** - System directories (`/etc`, `/usr`, `/var`, etc.) are protected
3. **TOCTOU protection** - Re-validates files immediately before deletion
4. **Symlink protection** - Detects and blocks symlink escape attempts
5. **Complete audit trail** - Every decision is logged with reasoning
6. **Multiple safety gates** - Files must pass policy, safety, and execution checks

---

## Quick Reference

### Start Everything
```bash
cd storage-sage
podman-compose up -d
```

### Stop Everything
```bash
podman-compose down
```

### Manual Cleanup Trigger
```bash
curl -X POST http://localhost:8080/trigger
```

### Check Status
```bash
curl http://localhost:8080/status
```

### View Logs
```bash
podman logs -f storage-sage
```

### Access Points
- Web UI: http://localhost:8080
- Grafana: http://localhost:3000 (admin/changeme)
- Prometheus: http://localhost:9090
