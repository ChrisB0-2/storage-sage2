// Package trash provides soft-delete functionality by moving files
// to a trash directory instead of permanently deleting them.
package trash

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/logger"
)

// Manager handles soft-delete operations by moving files to a trash directory.
type Manager struct {
	trashPath    string
	maxAge       time.Duration
	signingKey   []byte   // HMAC key for metadata integrity
	allowedRoots []string // Paths that can be restored to (empty = any)
	log          logger.Logger
}

// Config configures the trash manager.
type Config struct {
	// TrashPath is the directory where deleted files are moved.
	// If empty, soft-delete is disabled and files are permanently deleted.
	TrashPath string

	// MaxAge is the maximum age of trashed files before they are permanently deleted.
	// Zero means files are kept forever (manual cleanup required).
	MaxAge time.Duration

	// SigningKey is the HMAC key for metadata integrity verification.
	// If empty, a random key is generated (metadata won't survive restarts).
	// For production, set this to a persistent secret.
	SigningKey []byte

	// AllowedRoots restricts which paths files can be restored to.
	// If empty, restoration is allowed to any absolute path.
	// For security, set this to your scan roots.
	AllowedRoots []string
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

	// Ensure trash directory exists with secure permissions (owner only)
	if err := os.MkdirAll(cfg.TrashPath, 0700); err != nil {
		return nil, fmt.Errorf("creating trash directory: %w", err)
	}

	// Generate signing key if not provided
	signingKey := cfg.SigningKey
	if len(signingKey) == 0 {
		signingKey = make([]byte, 32)
		if _, err := rand.Read(signingKey); err != nil {
			return nil, fmt.Errorf("generating signing key: %w", err)
		}
		log.Warn("using ephemeral signing key - trash metadata will be unverifiable after restart")
	}

	return &Manager{
		trashPath:    cfg.TrashPath,
		maxAge:       cfg.MaxAge,
		signingKey:   signingKey,
		allowedRoots: cfg.AllowedRoots,
		log:          log,
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

	// Create signed metadata
	metaPath := trashPath + ".meta"
	trashedAt := time.Now().Format(time.RFC3339)
	metaContent := fmt.Sprintf("original_path: %s\ntrashed_at: %s\nsize: %d\nmode: %s\nmod_time: %s",
		path,
		trashedAt,
		info.Size(),
		info.Mode().String(),
		info.ModTime().Format(time.RFC3339),
	)
	// Add HMAC signature to prevent tampering
	signature := m.signMetadata(metaContent)
	meta := metaContent + "\nsignature: " + signature + "\n"

	// Move the file/directory
	if err := os.Rename(path, trashPath); err != nil {
		// If rename fails (cross-device), fall back to copy+delete
		if err := copyAndDelete(path, trashPath, info); err != nil {
			return "", fmt.Errorf("move to trash failed: %w", err)
		}
	}

	// Write metadata with secure permissions (owner only)
	if err := os.WriteFile(metaPath, []byte(meta), 0600); err != nil {
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
// Returns an error if metadata signature is invalid or path is not allowed.
func (m *Manager) Restore(trashPath string) (originalPath string, err error) {
	if m == nil {
		return "", fmt.Errorf("trash manager is nil")
	}

	// Verify trash path is within our trash directory (prevent path traversal)
	cleanTrashPath := filepath.Clean(trashPath)
	if !strings.HasPrefix(cleanTrashPath, m.trashPath+string(os.PathSeparator)) && cleanTrashPath != m.trashPath {
		return "", fmt.Errorf("invalid trash path: not within trash directory")
	}

	// Read metadata
	metaPath := trashPath + ".meta"
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return "", fmt.Errorf("reading trash metadata: %w", err)
	}

	// Parse metadata and extract signature
	var signature string
	var metaLines []string
	for _, line := range strings.Split(string(metaData), "\n") {
		if strings.HasPrefix(line, "original_path: ") {
			originalPath = strings.TrimPrefix(line, "original_path: ")
		}
		if strings.HasPrefix(line, "signature: ") {
			signature = strings.TrimPrefix(line, "signature: ")
		} else if line != "" {
			metaLines = append(metaLines, line)
		}
	}

	if originalPath == "" {
		return "", fmt.Errorf("original path not found in metadata")
	}

	// Verify HMAC signature to detect tampering
	if signature == "" {
		return "", fmt.Errorf("metadata signature missing - possible tampering")
	}
	metaContent := strings.Join(metaLines, "\n")
	if !m.verifyMetadata(metaContent, signature) {
		return "", fmt.Errorf("metadata signature invalid - tampering detected")
	}

	// Validate original path is absolute and clean
	if !filepath.IsAbs(originalPath) {
		return "", fmt.Errorf("original path must be absolute: %q", originalPath)
	}
	cleanOriginal := filepath.Clean(originalPath)
	if cleanOriginal != originalPath {
		return "", fmt.Errorf("original path is not clean: %q", originalPath)
	}

	// Validate path is within allowed roots (if configured)
	if len(m.allowedRoots) > 0 {
		allowed := false
		for _, root := range m.allowedRoots {
			if strings.HasPrefix(cleanOriginal, root+string(os.PathSeparator)) || cleanOriginal == root {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("restore path not within allowed roots: %q", originalPath)
		}
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

// signMetadata generates an HMAC-SHA256 signature for metadata content.
func (m *Manager) signMetadata(content string) string {
	mac := hmac.New(sha256.New, m.signingKey)
	mac.Write([]byte(content))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifyMetadata checks if the signature matches the content.
func (m *Manager) verifyMetadata(content, signature string) bool {
	expected := m.signMetadata(content)
	return hmac.Equal([]byte(expected), []byte(signature))
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

		// Calculate actual size (for directories, walk contents)
		var size int64
		if entry.IsDir() {
			size = calcDirSize(path)
		} else {
			size = info.Size()
		}

		item := TrashItem{
			TrashPath: path,
			Name:      entry.Name(),
			Size:      size,
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

// calcDirSize calculates the total size of all files in a directory.
func calcDirSize(path string) int64 {
	var size int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, _ error) error {
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size
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

// copyFileAndDelete copies a file using streaming I/O to avoid loading
// the entire file into memory. This prevents OOM when moving large files
// across filesystems under disk pressure.
func copyFileAndDelete(src, dst string, mode os.FileMode) error {
	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	// Write to temp file first for atomicity
	dstTmp := dst + ".tmp"
	dstFile, err := os.OpenFile(dstTmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create temp dest: %w", err)
	}

	// Stream copy with default 32KB buffer (io.Copy handles this)
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		dstFile.Close()
		os.Remove(dstTmp)
		return fmt.Errorf("copy data: %w", err)
	}

	// Sync to disk for durability
	if err := dstFile.Sync(); err != nil {
		dstFile.Close()
		os.Remove(dstTmp)
		return fmt.Errorf("sync: %w", err)
	}

	if err := dstFile.Close(); err != nil {
		os.Remove(dstTmp)
		return fmt.Errorf("close dest: %w", err)
	}

	// Atomic rename temp to final destination
	if err := os.Rename(dstTmp, dst); err != nil {
		os.Remove(dstTmp)
		return fmt.Errorf("rename temp: %w", err)
	}

	// Only remove source after successful copy
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

		// Use streaming copy to avoid OOM on large files
		return copyFileStreaming(path, dstPath, info.Mode())
	})

	if err != nil {
		return err
	}

	return os.RemoveAll(src)
}

// copyFileStreaming copies a single file using streaming I/O.
// Does not delete the source (used by copyDirAndDelete which does bulk removal).
func copyFileStreaming(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		dstFile.Close()
		os.Remove(dst)
		return fmt.Errorf("copy data: %w", err)
	}

	if err := dstFile.Sync(); err != nil {
		dstFile.Close()
		os.Remove(dst)
		return fmt.Errorf("sync: %w", err)
	}

	return dstFile.Close()
}
