#!/bin/bash
# Storage-Sage Smoke Test
# Verifies the Docker stack is working correctly
# Run after: docker compose up -d

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

pass() { echo -e "${GREEN}OK${NC}"; }
fail() { echo -e "${RED}FAIL${NC}"; exit 1; }

echo "=== Storage-Sage Smoke Test ==="
echo ""

# 1. Health check
echo -n "[1/9] Health endpoint... "
curl -sf http://localhost:8080/health > /dev/null && pass || fail

# 2. Ready check
echo -n "[2/9] Ready endpoint... "
curl -sf http://localhost:8080/ready > /dev/null && pass || fail

# 3. Status check
echo -n "[3/9] Status endpoint... "
STATUS=$(curl -sf http://localhost:8080/status 2>/dev/null)
echo "$STATUS" | grep -q '"state"' && pass || fail

# 4. Create test file (old enough to be deleted)
echo -n "[4/9] Creating test file... "
mkdir -p data
dd if=/dev/zero of=data/smoke_test_$(date +%s).tmp bs=1K count=100 2>/dev/null
TESTFILE=$(ls -t data/smoke_test_*.tmp 2>/dev/null | head -1)
if [ -n "$TESTFILE" ]; then
    touch -d "60 days ago" "$TESTFILE"
    chmod 666 "$TESTFILE"
    pass
else
    fail
fi

# 5. Trigger cleanup
echo -n "[5/9] Triggering cleanup... "
RESULT=$(curl -sf -X POST http://localhost:8080/trigger 2>/dev/null)
echo "$RESULT" | grep -q '"triggered":true' && pass || fail

# 6. Wait for cleanup to complete
echo -n "[6/9] Waiting for cleanup... "
sleep 3
pass

# 7. Check audit stats
echo -n "[7/9] Audit stats API... "
STATS=$(curl -sf http://localhost:8080/api/audit/stats 2>/dev/null)
echo "$STATS" | grep -q '"TotalRecords":' && pass || fail

# 8. Verify metrics
echo -n "[8/9] Metrics endpoint... "
METRICS=$(curl -sf http://localhost:9091/metrics 2>/dev/null)
echo "$METRICS" | grep -q "storagesage_scanner_files_scanned_total" && pass || fail

# 9. Prometheus scraping
echo -n "[9/9] Prometheus targets... "
TARGETS=$(curl -sf http://localhost:9090/api/v1/targets 2>/dev/null)
echo "$TARGETS" | grep -q '"health":"up"' && pass || fail

echo ""
echo -e "${GREEN}=== ALL CHECKS PASSED ===${NC}"
echo ""
echo "Service URLs:"
echo "  Dashboard:  http://localhost:8080"
echo "  Grafana:    http://localhost:3000 (admin/changeme)"
echo "  Prometheus: http://localhost:9090"
echo "  Metrics:    http://localhost:9091/metrics"
