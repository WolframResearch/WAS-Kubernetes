package tui

import (
	"fmt"
	"strings"
	"time"
)

// formatElapsed formats a duration as h:mm:ss.
func formatElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// padRight pads s with spaces to width w (visual width, ASCII-only safe).
func padRight(s string, w int) string {
	n := w - len(s)
	if n <= 0 {
		return s
	}
	return s + strings.Repeat(" ", n)
}

// truncate cuts s to at most n runes.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
