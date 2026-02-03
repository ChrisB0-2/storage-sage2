# Storage-Sage Demo Plan

A 10-12 minute guided demonstration for senior systems engineers.

**Goal**: Prove that Storage-Sage safely handles filesystem cleanup with full audit trail and recoverability.

---

## Pre-Demo Setup (Before Audience Arrives)

```bash
# Clean slate
cd /path/to/storage-sage
docker compose down -v 2>/dev/null || true
rm -rf data/* trash/* 2>/dev/null || true
mkdir -p data trash

# Verify ports are free
ss -tlnp | grep -E '(3000|3100|8080|9090|9091)' && echo "WARNING: Ports in use"

# Pre-pull images (avoid download time during demo)
docker compose pull

# Optionally pre-build to save time
docker compose build storage-sage
```

---

## Demo Script

### Minute 0-1: Introduction

**Say**: "Storage-Sage is a safety-first cleanup daemon. Unlike typical cleanup tools that delete first and regret later, Storage-Sage uses four independent safety gates and never permanently deletes anything without explicit confirmation. Let me show you."

**Show**: The README.md safety diagram (if projecting) or verbally describe:
- Gate 1: Policy (age, size, extension rules)
- Gate 2: Scan-time safety (protected paths)
- Gate 3: Symlink escape detection
- Gate 4: TOCTOU re-check at delete time

---

### Minute 1-3: Start the Stack

**Run**:
```bash
docker compose up -d
```

**Say**: "One command brings up the entire observability stack - the daemon, Prometheus for metrics, Grafana for dashboards, and Loki for centralized logging."

**Run**:
```bash
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
```

**Expected Output**:
```
NAMES         STATUS        PORTS
prometheus    Up            0.0.0.0:9090->9090/tcp
loki          Up            0.0.0.0:3100->3100/tcp
storage-sage  Up            0.0.0.0:8080->8080/tcp, 0.0.0.0:9091->9090/tcp
grafana       Up            0.0.0.0:3000->3000/tcp
```

**Verify Health**:
```bash
curl -s http://localhost:8080/health | jq .
```

**Expected**: `{"status":"ok","state":"ready"}`

**Say**: "All containers healthy. The daemon exposes three health endpoints: `/health` for liveness, `/ready` for readiness including disk space checks, and `/status` for operational state."

---

### Minute 3-5: Create Demo Files

**Say**: "Now let's simulate a real-world scenario. I'll create some files that should be protected and some that should be cleaned up."

**Run**:
```bash
# Protected files (recent)
echo "production_db_host=10.0.1.50" > data/config.yaml
echo "TODO: fix the login bug" > data/notes.txt
touch -d "5 days ago" data/recent_activity.log

# Cleanup candidates (old)
dd if=/dev/zero of=data/old_app.log bs=1M count=50 2>/dev/null
touch -d "60 days ago" data/old_app.log

dd if=/dev/zero of=data/build_artifacts.tar bs=1M count=100 2>/dev/null
touch -d "90 days ago" data/build_artifacts.tar

dd if=/dev/zero of=data/stale_backup.sql.gz bs=1M count=25 2>/dev/null
touch -d "45 days ago" data/stale_backup.sql.gz

# Set permissions for container access
chmod 666 data/*

echo "Created test files:"
ls -lh data/
echo ""
echo "Total size: $(du -sh data/ | cut -f1)"
```

**Say**: "We have about 175MB of old files mixed with recent work. The default policy is 30 days - anything older is eligible for cleanup, but recent files are protected."

---

### Minute 5-7: Trigger Cleanup and Observe

**Say**: "The daemon runs on a schedule, but we can trigger it manually via API."

**Run**:
```bash
curl -X POST http://localhost:8080/trigger | jq .
```

**Expected**: `{"triggered":true}`

**Run** (wait 2-3 seconds, then):
```bash
echo "=== PROTECTED (kept) ==="
ls -lh data/

echo ""
echo "=== TRASHED (recoverable) ==="
ls -lh trash/*.meta 2>/dev/null | head -5 || echo "(checking...)"
```

**Say**: "Notice the config file, notes, and recent log are still there - protected by policy. The old files were moved to trash, not deleted. Let's verify the audit trail."

**Run**:
```bash
curl -s http://localhost:8080/api/audit/stats | jq '{
  files_processed: .FilesProcessed,
  files_trashed: .FilesTrashed,
  bytes_freed: .TotalBytesFreed,
  total_audit_records: .TotalRecords
}'
```

---

### Minute 7-9: Prove Recoverability

**Say**: "The key safety feature: everything is recoverable. Let's restore one of the trashed files."

**Run**:
```bash
# List what's in trash
curl -s http://localhost:8080/api/trash | jq '.[] | {name, original_path, size_mb: (.size/1048576)}'
```

**Run**:
```bash
# Restore the backup file
BACKUP_NAME=$(curl -s http://localhost:8080/api/trash | jq -r '.[] | select(.original_path | contains("backup")) | .name')
echo "Restoring: $BACKUP_NAME"

curl -X POST http://localhost:8080/api/trash/restore \
  -H "Content-Type: application/json" \
  -d "{\"name\": \"$BACKUP_NAME\"}" | jq .
```

**Expected**: `{"restored":true,"original_path":"/data/stale_backup.sql.gz"}`

**Verify**:
```bash
ls -lh data/stale_backup.sql.gz
```

**Say**: "The file is back in its original location, with original permissions. Full undo capability."

---

### Minute 9-11: Show Observability

**Say**: "For production operations, you need visibility. Let's look at the metrics and dashboards."

**Run**:
```bash
# Key operational metrics
curl -s http://localhost:9091/metrics | grep -E "^storagesage_(executor|scanner|planner)" | head -10
```

**Say**: "These metrics feed into Prometheus. Let's verify scraping is working."

**Run**:
```bash
curl -s http://localhost:9090/api/v1/targets | jq '.data.activeTargets[] | {job: .labels.job, health}'
```

**Expected**: Both targets show `"health": "up"`

**Open in browser** (if possible):
- http://localhost:8080 - Dashboard showing run history
- http://localhost:3000 - Grafana (admin/changeme) with pre-configured dashboard

**Say**: "The web UI shows audit history, trash contents, and run status. Grafana provides time-series visualization and alerting."

---

### Minute 11-12: Safety Architecture Summary

**Say**: "Let me recap the safety guarantees:"

1. **Soft-delete by default** - Files go to trash, not permanent deletion
2. **Policy gates** - Age, size, and extension filtering
3. **Protected paths** - `/etc`, `/boot`, `/usr` cannot be touched
4. **Symlink protection** - Cannot escape the allowed root via symlinks
5. **TOCTOU protection** - Re-checks safety immediately before each delete
6. **Full audit trail** - Every decision logged with reasoning
7. **Disk-aware** - Auto-cleans trash at 90% usage, bypasses at 95%

**Run**:
```bash
curl -s http://localhost:8080/status | jq '{
  state,
  scheduler_enabled,
  run_count,
  last_error
}'
```

**Say**: "Questions?"

---

## Q&A Cheat Sheet

### "What happens if the daemon crashes mid-cleanup?"
**Answer**: Each file operation is atomic (move or delete). If the daemon crashes, it simply resumes on restart. The audit log shows exactly what was processed. Partial state is impossible.

### "Can this delete system files?"
**Answer**: No. Protected paths (`/boot`, `/etc`, `/usr`, `/var`, `/sys`, `/proc`, `/dev`) are hardcoded. Even if you accidentally mount `/`, these paths are blocked at the safety layer.

### "What if someone creates a symlink to escape the root?"
**Answer**: Gate 3 uses `lstat` to check every ancestor directory. If any component is a symlink pointing outside the allowed root, deletion is blocked and logged.

### "How do I know what will be deleted before it happens?"
**Answer**: Run with `-mode=dry-run` (the default). It shows the plan without executing. The web UI also shows what's eligible.

### "What about race conditions?"
**Answer**: Gate 4 re-runs all safety checks immediately before `os.Remove()`. If the file changed between scan and delete (TOCTOU attack), it's blocked.

### "What if disk fills up completely?"
**Answer**: At 90% usage, the daemon auto-purges old trash items. At 95%, it bypasses trash entirely and deletes directly to free space immediately.

### "Can I integrate this with my alerting?"
**Answer**: Yes. Prometheus metrics + included alerting rules. The `/ready` endpoint returns 503 when disk is critically full - use for load balancer health checks.

### "Why do Grafana and the Web UI show different numbers?"
**Answer**: They use different data sources. The Web UI reads from the SQLite audit database which persists across container restarts (all-time totals). Grafana reads from Prometheus which resets on container restart (current instance only). Both are correct - they just measure different time ranges.

### "Is the audit tamper-proof?"
**Answer**: Each SQLite record includes a SHA256 checksum. Run `storage-sage verify -db audit.db` to detect tampering.

---

## Panic Plan: When Things Go Wrong

### Container Won't Start

```bash
# Check what's blocking
docker compose logs storage-sage | tail -20

# Port conflict?
ss -tlnp | grep 8080

# Permission issue?
ls -la data/ trash/
# Fix: chmod 777 data trash
```

### API Returns Errors

```bash
# Check container is actually running
docker ps | grep storage-sage

# Check health internally
docker exec storage-sage wget -q -O- http://localhost:8080/health

# Check logs for panic
docker logs storage-sage 2>&1 | grep -i panic
```

### No Files Being Deleted

```bash
# Check policy - are files old enough?
ls -la --time=atime data/

# Default is 30 days. Check what policy sees:
docker logs storage-sage 2>&1 | grep "policy_allowed\|too_new"

# Force a file to be old enough
touch -d "60 days ago" data/testfile.tmp
```

### Grafana/Prometheus Not Working

```bash
# Check Prometheus can reach storage-sage
curl -s http://localhost:9090/api/v1/targets | jq '.data.activeTargets'

# If storage-sage target is down, check network
docker network inspect storage-sage_monitoring
```

### Full Reset (Nuclear Option)

```bash
# Complete teardown
docker compose down -v
rm -rf data/* trash/*
docker system prune -f

# Fresh start
mkdir -p data trash
docker compose up -d
```

### Prove Correctness Without UI

If the UI fails, demonstrate via CLI:

```bash
# 1. Health is OK
curl -s http://localhost:8080/health

# 2. Audit proves actions
curl -s "http://localhost:8080/api/audit/query?action=execute&limit=5" | jq .

# 3. Trash contents
curl -s http://localhost:8080/api/trash | jq '.[].original_path'

# 4. Metrics prove operation
curl -s http://localhost:9091/metrics | grep storagesage_executor_files_deleted_total

# 5. Logs show decisions
docker logs storage-sage 2>&1 | tail -30
```

---

## Post-Demo Cleanup

```bash
docker compose down -v
rm -rf data/* trash/*
```
