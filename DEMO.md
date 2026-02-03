# Storage-Sage Demo

A hands-on demo showing storage-sage protecting a system from disk bloat.

## Prerequisites

- Docker/Podman installed
- Ports 3000, 8080, 9090, 9091 available

## Quick Start (5 minutes)

### 1. Start Fresh

```bash
# Clone and enter the repo
cd storage-sage

# Clean slate - remove any existing state
docker compose down -v 2>/dev/null
rm -rf data/* trash/* 2>/dev/null
mkdir -p data trash

# Start the stack
docker compose up -d

# Verify all services are running
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
```

Expected output:
```
NAMES         STATUS         PORTS
prometheus    Up             0.0.0.0:9090->9090/tcp
loki          Up             0.0.0.0:3100->3100/tcp
storage-sage  Up             0.0.0.0:8080->8080/tcp, 0.0.0.0:9091->9090/tcp
grafana       Up             0.0.0.0:3000->3000/tcp
```

### 2. Open the Web UI

```bash
# Open the storage-sage dashboard
xdg-open http://localhost:8080   # Linux
open http://localhost:8080       # macOS
```

You should see an empty dashboard - no files processed yet.

### 3. Create Test Files (Simulating Real Usage)

```bash
# === FILES THAT WILL BE PROTECTED (too new) ===
echo "Important config" > data/config.yaml
echo "Recent work" > data/notes.txt
touch -d "5 days ago" data/recent_log.log

# === FILES THAT WILL BE CLEANED UP (old) ===
# Old logs (45+ days)
dd if=/dev/zero of=data/app.log.2024-01-15 bs=1M count=50 2>/dev/null
touch -d "60 days ago" data/app.log.2024-01-15

# Ancient temp files (90+ days)
dd if=/dev/zero of=data/temp_build_cache.tar bs=1M count=100 2>/dev/null
touch -d "90 days ago" data/temp_build_cache.tar

# Old backups (45+ days)
dd if=/dev/zero of=data/backup_old.sql.gz bs=1M count=25 2>/dev/null
touch -d "45 days ago" data/backup_old.sql.gz

# Stale uploads (60+ days)
dd if=/dev/zero of=data/upload_abc123.tmp bs=1M count=10 2>/dev/null
touch -d "60 days ago" data/upload_abc123.tmp

# Fix permissions for container access
chmod 777 data/*

# Show what we created
echo "=== Test Files Created ==="
ls -lah data/
echo ""
echo "Total disk usage:"
du -sh data/
```

### 4. Watch the Cleanup Happen

The daemon runs every 1 minute. You can either wait or trigger manually:

```bash
# Option A: Trigger immediately
curl -X POST http://localhost:8080/trigger
echo ""

# Option B: Watch logs for next scheduled run
docker logs -f storage-sage
```

### 5. See the Results

```bash
echo "=== PROTECTED FILES (still in data/) ==="
ls -lah data/
echo ""

echo "=== CLEANED UP FILES (moved to trash/) ==="
ls -lah trash/
echo ""

echo "=== AUDIT STATS ==="
curl -s http://localhost:8080/api/audit/stats | jq '.'
```

Expected results:
- `config.yaml`, `notes.txt`, `recent_log.log` → **PROTECTED** (too new)
- `app.log.2024-01-15`, `temp_build_cache.tar`, etc. → **TRASHED** (~185 MB freed)

### 6. Explore the UI

| URL | Description |
|-----|-------------|
| http://localhost:8080 | Storage-sage dashboard |
| http://localhost:8080 (History tab) | Audit log with decisions |
| http://localhost:8080 (Trash tab) | Recoverable deleted files |
| http://localhost:3000 | Grafana dashboards (admin/changeme) |
| http://localhost:9090 | Prometheus metrics |

### 7. Restore a File (Undo)

```bash
# List trashed files
curl -s http://localhost:8080/api/trash | jq '.[] | {name, original_path, size}'

# Restore one (use actual filename from trash)
FILENAME=$(ls trash/ | grep backup | head -1)
curl -X POST http://localhost:8080/api/trash/restore \
  -H "Content-Type: application/json" \
  -d "{\"name\": \"$FILENAME\"}"

# Verify it's back
ls -la data/backup*
```

### 8. View Metrics

```bash
# Key Prometheus metrics
curl -s http://localhost:9091/metrics | grep storagesage

# Important metrics:
# - storagesage_executor_files_deleted_total
# - storagesage_executor_bytes_freed_total
# - storagesage_scanner_files_scanned_total
# - storagesage_daemon_last_run_timestamp_seconds
```

## How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│                        STORAGE-SAGE                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   ┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐     │
│   │ SCANNER │───▶│ POLICY  │───▶│ SAFETY  │───▶│EXECUTOR │     │
│   │         │    │         │    │         │    │         │     │
│   │ Finds   │    │ Age>30d?│    │Protected│    │ Move to │     │
│   │ files   │    │ Size?   │    │ path?   │    │ trash   │     │
│   │         │    │ Ext?    │    │ Symlink?│    │         │     │
│   └─────────┘    └─────────┘    └─────────┘    └─────────┘     │
│                                                                 │
│   4 Safety Gates - Files must pass ALL to be deleted:          │
│   1. Policy rules (age, size, extension)                       │
│   2. Scan-time safety (protected paths, allowed roots)         │
│   3. Symlink check (prevents escape via symlinks)              │
│   4. Execute-time re-check (TOCTOU protection)                 │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Configuration

Default policy (from docker-compose.yml):
- **Min age**: 30 days (files younger are protected)
- **Schedule**: Every 1 minute
- **Mode**: Execute (actually deletes; use `dry-run` to preview)
- **Trash**: Enabled (soft-delete with recovery)

To customize, create a config file:
```bash
storage-sage init  # Creates ~/.config/storage-sage/config.yaml
```

## Cleanup

```bash
# Stop everything
docker compose down

# Full cleanup including data
docker compose down -v
rm -rf data/* trash/*
```

## Key Takeaways

1. **Safety-first**: 4 independent safety gates prevent accidental deletion
2. **Soft-delete**: Files go to trash first, can be restored
3. **Full audit trail**: Every decision logged with reasoning
4. **Observable**: Prometheus metrics + Grafana dashboards
5. **Fail-closed**: When in doubt, don't delete
