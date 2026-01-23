// Package trash provides soft-delete functionality by moving files
// to a trash directory instead of permanently deleting them.
package trash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/logger"
)

// Manager handles soft-delete operations by moving files to a trash directory.
type Manager struct {
	trashPath string
	maxAge    time.Duration
	log       logger.Logger
}

// Config configures the trash manager.
type Config struct {
	// TrashPath is the directory where deleted files are moved.
	// If empty, soft-delete is disabled and files are permanently deleted.
	TrashPath string

	// MaxAge is the maximum age of trashed files before they are permanently deleted.
	// Zero means files are kept forever (manual cleanup required).
	MaxAge time.Duration
}

// New creates a new trash manager.
// Returns nil if trash is disabled (empty TrashPath).
func New(cfg Config, log logger.Logger) (*Manager, error) {
	if cfg.TrashPath == "" {
		return nil, nil
	}

	if log == nil {
		log = logger.NewNop()
	}

	// Ensure trash directory exists
	if err := os.MkdirAll(cfg.TrashPath, 0755); err != nil {
		return nil, fmt.Errorf("creating trash directory: %w", err)
	}

	return &Manager{
		trashPath: cfg.TrashPath,
		maxAge:    cfg.MaxAge,
		log:       log,
	}, nil
}

// MoveToTrash moves a file or directory to the trash.
// Returns the path in the trash where the item was moved.
func (m *Manager) MoveToTrash(path string) (trashPath string, err error) {
	if m == nil {
		return "", fmt.Errorf("trash manager is nil (soft-delete disabled)")
	}

	// Get file info for metadata
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("stat failed: %w", err)
	}

	// Generate a unique name to avoid collisions
	// Format: YYYYMMDD-HHMMSS_hash_originalname
	timestamp := time.Now().Format("20060102-150405")
	hash := hashPath(path)
	baseName := filepath.Base(path)

	// Sanitize the base name (remove path separators that might cause issues)
	safeName := strings.ReplaceAll(baseName, string(os.PathSeparator), "_")
	if len(safeName) > 100 {
		safeName = safeName[:100] // Truncate very long names
	}

	trashName := fmt.Sprintf("%s_%s_%s", timestamp, hash[:8], safeName)
	trashPath = filepath.Join(m.trashPath, trashName)

	// Create metadata file
	metaPath := trashPath + ".meta"
	meta := fmt.Sprintf("original_path: %s\ntrashed_at: %s\nsize: %d\nmode: %s\nmod_time: %s\n",
		path,
		time.Now().Format(time.RFC3339),
		info.Size(),
		info.Mode().String(),
		info.ModTime().Format(time.RFC3339),
	)

	// Move the file/directory
	if err := os.Rename(path, trashPath); err != nil {
		// If rename fails (cross-device), fall back to copy+delete
		if err := copyAndDelete(path, trashPath, info); err != nil {
			return "", fmt.Errorf("move to trash failed: %w", err)
		}
	}

	// Write metadata (best effort - don't fail if this fails)
	if err := os.WriteFile(metaPath, []byte(meta), 0644); err != nil {
		m.log.Warn("failed to write trash metadata", logger.F("path", metaPath), logger.F("error", err.Error()))
	}

	m.log.Debug("moved to trash", logger.F("original", path), logger.F("trash", trashPath))

	return trashPath, nil
}

// Cleanup removes files from trash that are older than maxAge.
// Returns the number of items removed and bytes freed.
func (m *Manager) Cleanup(ctx context.Context) (count int, bytesFreed int64, err error) {
	if m == nil || m.maxAge == 0 {
		return 0, 0, nil // No cleanup needed
	}

	cutoff := time.Now().Add(-m.maxAge)

	err = filepath.WalkDir(m.trashPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // Skip errors
		}

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip the trash root and metadata files
		if path == m.trashPath || strings.HasSuffix(path, ".meta") {
			return nil
		}

		// Only process top-level items in trash
		if filepath.Dir(path) != m.trashPath {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Check if older than cutoff (use mod time which was set when trashed)
		if info.ModTime().Before(cutoff) {
			var size int64

			if d.IsDir() {
				// Calculate directory size
				_ = filepath.WalkDir(path, func(_ string, de fs.DirEntry, _ error) error {
					if !de.IsDir() {
						if fi, err := de.Info(); err == nil {
							size += fi.Size()
						}
					}
					return nil
				})
				err = os.RemoveAll(path)
			} else {
				size = info.Size()
				err = os.Remove(path)
			}

			if err != nil {
				m.log.Warn("failed to cleanup trash item", logger.F("path", path), logger.F("error", err.Error()))
				return nil
			}

			// Also remove metadata file
			_ = os.Remove(path + ".meta")

			count++
			bytesFreed += size

			m.log.Debug("removed expired trash item", logger.F("path", path), logger.F("age", time.Since(info.ModTime())))
		}

		return nil
	})

	if err != nil && err != context.Canceled {
		return count, bytesFreed, fmt.Errorf("trash cleanup walk failed: %w", err)
	}

	if count > 0 {
		m.log.Info("trash cleanup completed", logger.F("items_removed", count), logger.F("bytes_freed", bytesFreed))
	}

	return count, bytesFreed, nil
}

// Restore restores a file from trash to its original location.
func (m *Manager) Restore(trashPath string) (originalPath string, err error) {
	if m == nil {
		return "", fmt.Errorf("trash manager is nil")
	}

	// Read metadata to get original path
	metaPath := trashPath + ".meta"
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return "", fmt.Errorf("reading trash metadata: %w", err)
	}

	// Parse original path from metadata
	for _, line := range strings.Split(string(metaData), "\n") {
		if strings.HasPrefix(line, "original_path: ") {
			originalPath = strings.TrimPrefix(line, "original_path: ")
			break
		}
	}

	if originalPath == "" {
		return "", fmt.Errorf("original path not found in metadata")
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(originalPath), 0755); err != nil {
		return "", fmt.Errorf("creating parent directory: %w", err)
	}

	// Move back
	if err := os.Rename(trashPath, originalPath); err != nil {
		return "", fmt.Errorf("restore failed: %w", err)
	}

	// Remove metadata file
	_ = os.Remove(metaPath)

	m.log.Info("restored from trash", logger.F("trash", trashPath), logger.F("original", originalPath))

	return originalPath, nil
}

// List returns all items currently in trash.
func (m *Manager) List() ([]TrashItem, error) {
	if m == nil {
		return nil, nil
	}

	var items []TrashItem

	entries, err := os.ReadDir(m.trashPath)
	if err != nil {
		return nil, fmt.Errorf("reading trash directory: %w", err)
	}

	for _, entry := range entries {
		// Skip metadata files
		if strings.HasSuffix(entry.Name(), ".meta") {
			continue
		}

		path := filepath.Join(m.trashPath, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		item := TrashItem{
			TrashPath: path,
			Name:      entry.Name(),
			Size:      info.Size(),
			TrashedAt: info.ModTime(),
			IsDir:     entry.IsDir(),
		}

		// Try to read original path from metadata
		if metaData, err := os.ReadFile(path + ".meta"); err == nil {
			for _, line := range strings.Split(string(metaData), "\n") {
				if strings.HasPrefix(line, "original_path: ") {
					item.OriginalPath = strings.TrimPrefix(line, "original_path: ")
					break
				}
			}
		}

		items = append(items, item)
	}

	return items, nil
}

// TrashItem represents an item in the trash.
type TrashItem struct {
	TrashPath    string
	OriginalPath string
	Name         string
	Size         int64
	TrashedAt    time.Time
	IsDir        bool
}

// hashPath generates a short hash of the path for unique naming.
func hashPath(path string) string {
	h := sha256.Sum256([]byte(path))
	return hex.EncodeToString(h[:])
}

// copyAndDelete copies a file/directory and then deletes the original.
// Used when rename fails (e.g., cross-device move).
func copyAndDelete(src, dst string, info os.FileInfo) error {
	if info.IsDir() {
		return copyDirAndDelete(src, dst)
	}
	return copyFileAndDelete(src, dst, info.Mode())
}

func copyFileAndDelete(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	if err := os.WriteFile(dst, data, mode); err != nil {
		return err
	}

	return os.Remove(src)
}

func copyDirAndDelete(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})

	if err != nil {
		return err
	}

	return os.RemoveAll(src)
}
