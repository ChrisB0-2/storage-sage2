#!/bin/bash
# Create test files for Storage-Sage demo
# Usage: ./create-test-files.sh [data-directory]

DATA_DIR="${1:-./data}"
mkdir -p "$DATA_DIR"
cd "$DATA_DIR"

echo "=== Storage-Sage Test File Generator ==="
echo ""

echo "Creating old .tmp files (60 days old)..."
for i in {1..10}; do
  echo "Old test file $i - sample content for testing storage-sage cleanup" > "old_file_$i.tmp"
  touch -d "60 days ago" "old_file_$i.tmp"
done

echo "Creating old .log files (45 days old, ~135KB each)..."
for i in {1..5}; do
  dd if=/dev/urandom bs=1024 count=100 2>/dev/null | base64 > "old_log_$i.log"
  touch -d "45 days ago" "old_log_$i.log"
done

echo "Creating recent files (will NOT be deleted)..."
for i in {1..3}; do
  echo "Recent file $i - should not be deleted" > "recent_$i.txt"
done

echo ""
echo "=== Files Created ==="
ls -lah

echo ""
echo "=== Summary ==="
echo "Old files (will be deleted):    15"
echo "Recent files (protected):       3"
echo "Total:                          18"

echo ""
echo "=== Next Steps ==="
echo "1. Trigger cleanup:  curl -X POST http://localhost:8080/trigger"
echo "2. Watch logs:       podman logs -f storage-sage"
echo "3. Check results:    ls -la $DATA_DIR"
echo "4. View stats:       curl http://localhost:8080/api/audit/stats"
echo "5. Open Grafana:     http://localhost:3000"
