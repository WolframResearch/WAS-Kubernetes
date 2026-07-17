package collectors

import (
	"context"
	"fmt"
	"strings"
)

// CLIVersionsCollector collects version strings from installed CLI tools.
type CLIVersionsCollector struct{}

func (CLIVersionsCollector) Name() string { return "cli-versions" }

func (CLIVersionsCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	var sb strings.Builder

	tools := []struct {
		label string
		cmd   string
		args  []string
	}{
		{"terraform", "terraform", []string{"version"}},
		{"helm", "helm", []string{"version", "--short"}},
		{"kubectl", "kubectl", []string{"version", "--client", "--short"}},
		{"aws", "aws", []string{"--version"}},
		{"az", "az", []string{"version", "--output", "json"}},
	}

	for _, t := range tools {
		out, err := runOutput(ctx, t.cmd, t.args...)
		if err != nil {
			fmt.Fprintf(&sb, "%-12s  [not found or error: %s]\n", t.label, err)
		} else {
			line := strings.TrimSpace(string(out))
			// Take first line only for conciseness.
			if nl := strings.IndexByte(line, '\n'); nl > 0 {
				line = line[:nl]
			}
			fmt.Fprintf(&sb, "%-12s  %s\n", t.label, line)
		}
	}

	return []File{text("system/cli_versions.txt", sb.String())}, nil
}
