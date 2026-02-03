#!/bin/bash
# Storage-Sage Demo Script
# Run this to see storage-sage in action from scratch

set -e

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

print_step() {
    echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}▶ $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

# Step 1: Clean slate
print_step "Step 1: Starting fresh"
docker compose down -v 2>/dev/null || true
rm -rf data/* trash/* 2>/dev/null || true
mkdir -p data trash
echo "Cleaned up previous state"

# Step 2: Start services
print_step "Step 2: Starting storage-sage stack"
docker compose up -d
sleep 3
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

# Step 3: Create test files
print_step "Step 3: Creating test files"

echo "Creating PROTECTED files (too new to delete)..."
echo "server_config=production" > data/config.yaml
echo "TODO: finish demo" > data/notes.txt
touch -d "5 days ago" data/recent_log.log
echo "  ✓ config.yaml (today)"
echo "  ✓ notes.txt (today)"
echo "  ✓ recent_log.log (5 days old)"

echo ""
echo "Creating OLD files (will be cleaned up)..."

dd if=/dev/zero of=data/app.log.old bs=1M count=50 2>/dev/null
touch -d "60 days ago" data/app.log.old
echo "  ✓ app.log.old (60 days, 50 MB)"

dd if=/dev/zero of=data/build_cache.tar bs=1M count=100 2>/dev/null
touch -d "90 days ago" data/build_cache.tar
echo "  ✓ build_cache.tar (90 days, 100 MB)"

dd if=/dev/zero of=data/backup.sql.gz bs=1M count=25 2>/dev/null
touch -d "45 days ago" data/backup.sql.gz
echo "  ✓ backup.sql.gz (45 days, 25 MB)"

dd if=/dev/zero of=data/upload_temp.tmp bs=1M count=10 2>/dev/null
touch -d "60 days ago" data/upload_temp.tmp
echo "  ✓ upload_temp.tmp (60 days, 10 MB)"

chmod 777 data/*

echo ""
echo "Disk usage before cleanup:"
du -sh data/

# Step 4: Trigger cleanup
print_step "Step 4: Triggering cleanup"
print_info "Storage-sage will now scan and clean old files..."
curl -s -X POST http://localhost:8080/trigger | jq '.'
sleep 3

# Step 5: Show results
print_step "Step 5: Results"

echo "=== PROTECTED FILES (kept in data/) ==="
ls -lah data/ 2>/dev/null || echo "(empty)"
echo ""

echo "=== CLEANED FILES (moved to trash/) ==="
ls -lh trash/*.meta 2>/dev/null | while read line; do
    file=$(echo "$line" | awk '{print $NF}' | sed 's/.meta$//')
    size=$(ls -lh "$file" 2>/dev/null | awk '{print $5}')
    name=$(basename "$file")
    echo "  $name ($size)"
done
echo ""

echo "=== STATS ==="
curl -s http://localhost:8080/api/audit/stats | jq '{
    files_deleted: .FilesDeleted,
    bytes_freed_mb: (.TotalBytesFreed / 1048576 | floor),
    errors: .Errors
}'
echo ""
echo "(audit_records: plan + execute events logged for all runs)"

# Step 6: Show UI links
print_step "Step 6: Explore the UI"
echo "Open these URLs in your browser:"
echo ""
echo "  Dashboard:    http://localhost:8080"
echo "  Grafana:      http://localhost:3000  (admin/changeme)"
echo "  Prometheus:   http://localhost:9090"
echo ""

print_info "Demo complete! The old files have been safely moved to trash."
print_info "Recent files (config.yaml, notes.txt, recent_log.log) were protected."
echo ""
echo "To restore a file:  curl -X POST http://localhost:8080/api/trash/restore -d '{\"name\":\"FILENAME\"}'"
echo "To stop:            docker compose down"
echo "To cleanup:         docker compose down -v && rm -rf data/* trash/*"
