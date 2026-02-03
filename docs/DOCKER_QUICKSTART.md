# Docker Quickstart

Get Storage-Sage running in under 2 minutes on a fresh VM.

## Prerequisites

| Requirement | Command to Verify |
|-------------|-------------------|
| Docker CE or Podman | `docker --version` or `podman --version` |
| Docker Compose v2 | `docker compose version` |
| Ports 3000, 3100, 8080, 9090, 9091 available | `ss -tlnp \| grep -E '(3000\|3100\|8080\|9090\|9091)'` |
| curl + jq (for smoke test) | `curl --version && jq --version` |

### Install Docker (if needed)

**Ubuntu/Debian:**
```bash
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER
newgrp docker
```

**RHEL/Rocky/Alma:**
```bash
sudo dnf install -y podman podman-compose
# podman-compose emulates docker compose
```

## One-Command Start

```bash
# Clone and start (from repo root)
git clone https://github.com/ChrisB0-2/storage-sage.git
cd storage-sage
mkdir -p data trash
docker compose up -d
```

Expected output:
```
Creating network "storage-sage_monitoring" with driver "bridge"
Creating loki ... done
Creating prometheus ... done
Creating storage-sage ... done
Creating grafana ... done
```

## Verify Stack Health

```bash
# All containers running?
docker compose ps

# API responding?
curl -s http://localhost:8080/health | jq .
# Expected: {"status":"ok","state":"ready"}

# Metrics flowing?
curl -s http://localhost:9091/metrics | grep storagesage | head -3
```

## Smoke Test (Full Verification)

Run this script to verify the complete pipeline works:

```bash
#!/bin/bash
set -e

echo "=== Storage-Sage Smoke Test ==="

# 1. Health check
echo -n "Health endpoint... "
curl -sf http://localhost:8080/health > /dev/null && echo "OK" || { echo "FAIL"; exit 1; }

# 2. Ready check
echo -n "Ready endpoint... "
curl -sf http://localhost:8080/ready > /dev/null && echo "OK" || { echo "FAIL"; exit 1; }

# 3. Create test file (old enough to be deleted)
echo -n "Creating test file... "
dd if=/dev/zero of=data/smoke_test_file.tmp bs=1K count=100 2>/dev/null
touch -d "60 days ago" data/smoke_test_file.tmp
chmod 666 data/smoke_test_file.tmp
echo "OK"

# 4. Trigger cleanup
echo -n "Triggering cleanup... "
RESULT=$(curl -sf -X POST http://localhost:8080/trigger)
echo "$RESULT" | grep -q '"triggered":true' && echo "OK" || { echo "FAIL: $RESULT"; exit 1; }

# 5. Wait for cleanup to complete
echo -n "Waiting for cleanup... "
sleep 3
echo "OK"

# 6. Verify file moved to trash
echo -n "Verifying trash... "
TRASH=$(curl -sf http://localhost:8080/api/trash)
echo "$TRASH" | grep -q "smoke_test_file" && echo "OK" || { echo "FAIL: file not in trash"; exit 1; }

# 7. Check audit stats
echo -n "Checking audit... "
STATS=$(curl -sf http://localhost:8080/api/audit/stats)
echo "$STATS" | grep -q '"TotalRecords":' && echo "OK" || { echo "FAIL"; exit 1; }

# 8. Verify metrics
echo -n "Checking metrics... "
METRICS=$(curl -sf http://localhost:9091/metrics)
echo "$METRICS" | grep -q "storagesage_scanner_files_scanned_total" && echo "OK" || { echo "FAIL"; exit 1; }

# 9. Prometheus scraping?
echo -n "Prometheus targets... "
TARGETS=$(curl -sf http://localhost:9090/api/v1/targets)
echo "$TARGETS" | grep -q '"health":"up"' && echo "OK" || { echo "FAIL"; exit 1; }

echo ""
echo "=== ALL CHECKS PASSED ==="
echo ""
echo "Access points:"
echo "  Dashboard:  http://localhost:8080"
echo "  Grafana:    http://localhost:3000 (admin/changeme)"
echo "  Prometheus: http://localhost:9090"
echo "  Metrics:    http://localhost:9091/metrics"
```

Save as `smoke-test.sh` and run:
```bash
chmod +x smoke-test.sh
./smoke-test.sh
```

## Service URLs

| Service | URL | Credentials |
|---------|-----|-------------|
| Storage-Sage Dashboard | http://localhost:8080 | None |
| Grafana | http://localhost:3000 | admin / changeme |
| Prometheus | http://localhost:9090 | None |
| Loki (API only) | http://localhost:3100 | None |
| Metrics | http://localhost:9091/metrics | None |

## Common Operations

### View Logs
```bash
# All services
docker compose logs -f

# Just storage-sage
docker compose logs -f storage-sage
```

### Trigger Manual Cleanup
```bash
curl -X POST http://localhost:8080/trigger
```

### Check Audit History
```bash
# Last 10 actions
curl -s "http://localhost:8080/api/audit/query?limit=10" | jq .

# Only deletions
curl -s "http://localhost:8080/api/audit/query?action=execute" | jq .
```

### List Trash
```bash
curl -s http://localhost:8080/api/trash | jq '.[] | {name, original_path, size}'
```

### Restore from Trash
```bash
curl -X POST http://localhost:8080/api/trash/restore \
  -H "Content-Type: application/json" \
  -d '{"name": "FILENAME_FROM_TRASH"}'
```

## Stop and Cleanup

```bash
# Stop services (keep data)
docker compose down

# Stop and remove all data
docker compose down -v
rm -rf data/* trash/*
```

## Understanding the Metrics

Storage-Sage has two dashboards that show different data:

| Dashboard | URL | Data Source | Persistence |
|-----------|-----|-------------|-------------|
| Web UI | http://localhost:8080 | SQLite audit DB | All-time (survives restarts) |
| Grafana | http://localhost:3000 | Prometheus | Current instance (resets on restart) |

**Why the numbers differ**: After a container restart, the Web UI shows cumulative all-time stats while Grafana shows only activity since the restart. This is by design - the audit database provides compliance history while Prometheus provides real-time operational metrics.

## Troubleshooting

### Container won't start
```bash
# Check for port conflicts
ss -tlnp | grep -E '(3000|3100|8080|9090|9091)'

# Check container logs
docker compose logs storage-sage
```

### Permission denied errors
```bash
# Ensure data directories are writable
chmod 777 data trash
```

### Podman-specific issues
```bash
# Ensure rootless mode is configured
podman info | grep rootless

# For SELinux systems, volumes need :z flag (already in compose)
```

### Health check failing
```bash
# Manual health check
docker exec storage-sage wget -q -O- http://localhost:8080/health
```
