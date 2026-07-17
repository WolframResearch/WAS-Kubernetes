package supportbundle

import (
	"fmt"
	"io"
	"time"
)

// ProgressWriter emits per-collector progress to a writer (typically stderr).
type ProgressWriter struct {
	w io.Writer
}

// NewProgressWriter returns a ProgressWriter that emits to w.
func NewProgressWriter(w io.Writer) *ProgressWriter {
	return &ProgressWriter{w: w}
}

// BundleStart prints the bundle header.
func (p *ProgressWriter) BundleStart(cluster string) {
	fmt.Fprintf(p.w, "\ncollecting support bundle  cluster: %s\n\n", cluster)
}

// CollectorStart is a no-op in the current plain-text renderer; each collector
// reports when it finishes. Reserved for TTY-mode cursor-up rendering.
func (p *ProgressWriter) CollectorStart(_ string) {}

// CollectorDone prints a success line for a completed collector.
func (p *ProgressWriter) CollectorDone(name string, elapsed time.Duration, fileCount int) {
	fmt.Fprintf(p.w, "  %-24s  %d file(s)  %s\n", name, fileCount, fmtElapsed(elapsed))
}

// CollectorFail prints a skip line when a collector returns an error.
func (p *ProgressWriter) CollectorFail(name string, err error) {
	fmt.Fprintf(p.w, "  %-24s  skipped: %s\n", name, err)
}

// BundleDone prints the final summary line.
func (p *ProgressWriter) BundleDone(path string, size int64) {
	fmt.Fprintf(p.w, "\n")
	fmt.Fprintf(p.w, "  bundle: %s  (%s)\n", path, fmtSize(size))
	fmt.Fprintf(p.w, "\n  Send to support@wolfram.com with a brief description.\n")
	fmt.Fprintf(p.w, "  Review the bundle before sending; sanitization is best-effort.\n\n")
}

func fmtElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func fmtSize(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(bytes)/1024/1024)
	}
}
