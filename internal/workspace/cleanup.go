package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CleanOrphanTempDirs removes /tmp/wasctl-* directories older than maxAge.
// It is called at wasctl startup to recover disk space from crashed prior runs.
// Returns the number of directories removed and the total bytes freed.
func CleanOrphanTempDirs(maxAge time.Duration) (count int, totalBytes int64) {
	pattern := filepath.Join(os.TempDir(), "wasctl-*")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return 0, 0
	}

	cutoff := time.Now().Add(-maxAge)
	for _, dir := range matches {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		size := dirSize(dir)
		if err := os.RemoveAll(dir); err == nil {
			count++
			totalBytes += size
		}
	}
	return count, totalBytes
}

// FormatCleanupLog returns a human-readable summary of a cleanup run.
// Returns an empty string if nothing was cleaned.
func FormatCleanupLog(count int, totalBytes int64) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("wasctl: startup GC removed %d orphan temp dir(s) (%s freed)", count, formatSize(totalBytes))
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func formatSize(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.0f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

