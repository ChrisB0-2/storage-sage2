package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// BenchmarkScan_SmallDir benchmarks scanning a directory with ~100 files
func BenchmarkScan_SmallDir(b *testing.B) {
	tmpDir := b.TempDir()
	createTestFiles(b, tmpDir, 100, 1024) // 100 files, 1KB each

	scanner := NewWalkDir()
	req := core.ScanRequest{
		Roots:        []string{tmpDir},
		Recursive:    true,
		MaxDepth:     0,
		IncludeFiles: true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cands, errc := scanner.Scan(ctx, req)

		// Drain channels
		count := 0
		for range cands {
			count++
		}
		if err := <-errc; err != nil {
			b.Fatalf("scan error: %v", err)
		}
		cancel()
	}
}

// BenchmarkScan_MediumDir benchmarks scanning a directory with ~1000 files
func BenchmarkScan_MediumDir(b *testing.B) {
	tmpDir := b.TempDir()
	createTestFiles(b, tmpDir, 1000, 1024) // 1000 files, 1KB each

	scanner := NewWalkDir()
	req := core.ScanRequest{
		Roots:        []string{tmpDir},
		Recursive:    true,
		MaxDepth:     0,
		IncludeFiles: true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		cands, errc := scanner.Scan(ctx, req)

		count := 0
		for range cands {
			count++
		}
		if err := <-errc; err != nil {
			b.Fatalf("scan error: %v", err)
		}
		cancel()
	}
}

// BenchmarkScan_LargeDir benchmarks scanning a directory with ~10000 files
func BenchmarkScan_LargeDir(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping large directory benchmark in short mode")
	}

	tmpDir := b.TempDir()
	createTestFiles(b, tmpDir, 10000, 512) // 10K files, 512 bytes each

	scanner := NewWalkDir()
	req := core.ScanRequest{
		Roots:        []string{tmpDir},
		Recursive:    true,
		MaxDepth:     0,
		IncludeFiles: true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		cands, errc := scanner.Scan(ctx, req)

		count := 0
		for range cands {
			count++
		}
		if err := <-errc; err != nil {
			b.Fatalf("scan error: %v", err)
		}
		cancel()
	}
}

// BenchmarkScan_DeepNesting benchmarks scanning deeply nested directories
func BenchmarkScan_DeepNesting(b *testing.B) {
	tmpDir := b.TempDir()
	createNestedDirs(b, tmpDir, 10, 5) // 10 levels deep, 5 files per level

	scanner := NewWalkDir()
	req := core.ScanRequest{
		Roots:        []string{tmpDir},
		Recursive:    true,
		MaxDepth:     0,
		IncludeFiles: true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cands, errc := scanner.Scan(ctx, req)

		count := 0
		for range cands {
			count++
		}
		if err := <-errc; err != nil {
			b.Fatalf("scan error: %v", err)
		}
		cancel()
	}
}

// BenchmarkScan_WithMaxDepth benchmarks scanning with depth limit
func BenchmarkScan_WithMaxDepth(b *testing.B) {
	tmpDir := b.TempDir()
	createNestedDirs(b, tmpDir, 20, 3) // 20 levels, but we'll only scan 5

	scanner := NewWalkDir()
	req := core.ScanRequest{
		Roots:        []string{tmpDir},
		Recursive:    true,
		MaxDepth:     5,
		IncludeFiles: true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cands, errc := scanner.Scan(ctx, req)

		count := 0
		for range cands {
			count++
		}
		if err := <-errc; err != nil {
			b.Fatalf("scan error: %v", err)
		}
		cancel()
	}
}

// BenchmarkScan_MultipleRoots benchmarks scanning multiple root directories
func BenchmarkScan_MultipleRoots(b *testing.B) {
	tmpDir1 := b.TempDir()
	tmpDir2 := b.TempDir()
	tmpDir3 := b.TempDir()

	createTestFiles(b, tmpDir1, 500, 1024)
	createTestFiles(b, tmpDir2, 500, 1024)
	createTestFiles(b, tmpDir3, 500, 1024)

	scanner := NewWalkDir()
	req := core.ScanRequest{
		Roots:        []string{tmpDir1, tmpDir2, tmpDir3},
		Recursive:    true,
		MaxDepth:     0,
		IncludeFiles: true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		cands, errc := scanner.Scan(ctx, req)

		count := 0
		for range cands {
			count++
		}
		if err := <-errc; err != nil {
			b.Fatalf("scan error: %v", err)
		}
		cancel()
	}
}

// createTestFiles creates n files of specified size in the directory
func createTestFiles(b *testing.B, dir string, n int, size int) {
	b.Helper()
	data := make([]byte, size)

	for i := 0; i < n; i++ {
		path := filepath.Join(dir, "file_"+string(rune('0'+i/1000))+string(rune('0'+(i/100)%10))+string(rune('0'+(i/10)%10))+string(rune('0'+i%10))+".tmp")
		if err := os.WriteFile(path, data, 0644); err != nil {
			b.Fatalf("failed to create test file: %v", err)
		}
		// Set modification time to be old enough for policy
		oldTime := time.Now().Add(-40 * 24 * time.Hour)
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			b.Fatalf("failed to set file time: %v", err)
		}
	}
}

// createNestedDirs creates a nested directory structure
func createNestedDirs(b *testing.B, base string, depth, filesPerLevel int) {
	b.Helper()
	data := make([]byte, 1024)
	current := base

	for d := 0; d < depth; d++ {
		// Create files at this level
		for f := 0; f < filesPerLevel; f++ {
			path := filepath.Join(current, "file_"+string(rune('0'+d))+"_"+string(rune('0'+f))+".tmp")
			if err := os.WriteFile(path, data, 0644); err != nil {
				b.Fatalf("failed to create test file: %v", err)
			}
		}

		// Create next level directory
		current = filepath.Join(current, "level_"+string(rune('0'+d)))
		if err := os.MkdirAll(current, 0755); err != nil {
			b.Fatalf("failed to create directory: %v", err)
		}
	}
}
