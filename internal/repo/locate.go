// Package repo locates the wasctl repository root by walking up the
// filesystem from a starting directory.
package repo

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrNotFound is returned when the repository root cannot be found.
var ErrNotFound = errors.New("repo root not found: no .git directory or go.mod found in parent directories")

// Locate walks up the directory tree from start looking for a .git directory
// or go.mod file that marks the repository root. It returns the absolute path
// to the first directory where either marker exists.
//
// If start is empty, the current working directory is used.
func Locate(start string) (string, error) {
	if start == "" {
		var err error
		start, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}

	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}

	for {
		if isRoot(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding a marker.
			return "", ErrNotFound
		}
		dir = parent
	}
}

// isRoot returns true if dir contains a .git directory or a go.mod file.
func isRoot(dir string) bool {
	for _, marker := range []string{".git", "go.mod"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}
