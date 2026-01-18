package web

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var distFS embed.FS

// DistFS returns the embedded filesystem containing the frontend build artifacts.
// Returns an fs.FS rooted at the dist/ directory.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// HasDist returns true if the dist directory contains files.
// This is used to check if the frontend has been built.
func HasDist() bool {
	entries, err := fs.ReadDir(distFS, "dist")
	if err != nil {
		return false
	}
	return len(entries) > 0
}
