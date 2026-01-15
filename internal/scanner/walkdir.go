package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
	"github.com/ChrisB0-2/storage-sage/internal/logger"
)

type WalkDirScanner struct {
	log logger.Logger
}

// NewWalkDir creates a scanner with no-op logging.
func NewWalkDir() *WalkDirScanner {
	return &WalkDirScanner{log: logger.NewNop()}
}

// NewWalkDirWithLogger creates a scanner with the given logger.
func NewWalkDirWithLogger(log logger.Logger) *WalkDirScanner {
	if log == nil {
		log = logger.NewNop()
	}
	return &WalkDirScanner{log: log}
}

// Scan walks each root and emits Candidates. It never deletes.
func (s *WalkDirScanner) Scan(ctx context.Context, req core.ScanRequest) (<-chan core.Candidate, <-chan error) {
	out := make(chan core.Candidate, 128)
	errc := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errc)

		s.log.Debug("scan starting", logger.F("roots", req.Roots), logger.F("max_depth", req.MaxDepth))

		for _, root := range req.Roots {
			root = filepath.Clean(root)
			if absRoot, err := filepath.Abs(root); err == nil {
				root = absRoot
			}

			walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				if req.MaxDepth > 0 {
					rel, relErr := filepath.Rel(root, path)
					if relErr == nil {
						depth := 0
						for _, r := range rel {
							if r == filepath.Separator {
								depth++
							}
						}
						if depth >= req.MaxDepth && d.IsDir() {
							return fs.SkipDir
						}
					}
				}

				var tt core.TargetType
				if d.IsDir() {
					tt = core.TargetDir
				} else {
					tt = core.TargetFile
				}

				if (tt == core.TargetDir && !req.IncludeDirs) || (tt == core.TargetFile && !req.IncludeFiles) {
					return nil
				}

				info, infoErr := d.Info()
				if infoErr != nil {
					return infoErr
				}
				size := int64(0)
				if !d.IsDir() {
					size = info.Size()
				}
				candPath := filepath.Clean(path)
				if absPath, err := filepath.Abs(candPath); err == nil {
					candPath = absPath
				}

				c := core.Candidate{
					Root:      root,
					Path:      candPath,
					Type:      tt,
					ModTime:   info.ModTime(),
					FoundAt:   time.Now(),
					SizeBytes: size,
				}

				if d.Type()&fs.ModeSymlink != 0 {
					c.IsSymlink = true

					// Record the symlink target for safety checks.
					if link, err := os.Readlink(path); err == nil {
						// If the link is relative, interpret it relative to the symlink's directory.
						if !filepath.IsAbs(link) {
							link = filepath.Join(filepath.Dir(path), link)
						}

						// Prefer absolute, cleaned target.
						if abs, err := filepath.Abs(link); err == nil {
							c.LinkTarget = abs
						} else {
							c.LinkTarget = filepath.Clean(link)
						}
					}
				}

				out <- c
				return nil
			})

			if walkErr != nil {
				s.log.Warn("scan error", logger.F("root", root), logger.F("error", walkErr.Error()))
				errc <- walkErr
				return
			}
			s.log.Debug("root scan complete", logger.F("root", root))
		}
		s.log.Debug("scan complete")
	}()

	return out, errc
}
