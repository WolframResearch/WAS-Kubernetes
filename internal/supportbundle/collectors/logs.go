package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// LogsCollector collects the last N lines of logs from each pod in the WAS namespace.
type LogsCollector struct{}

func (LogsCollector) Name() string { return "logs" }

func (LogsCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	if cc.Kubeconfig == "" {
		return nil, fmt.Errorf("no cluster access (kubeconfig unavailable)")
	}

	maxLines := cc.MaxLogLines
	if maxLines <= 0 {
		maxLines = 1000
	}

	kargs := kubectlArgs(cc)

	// Get pod list as JSON.
	podListJSON, err := runOutput(ctx, "kubectl",
		append(kargs, "get", "pods", "-n", wasNamespace, "-o", "json")...)
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	var podList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(podListJSON, &podList); err != nil {
		return nil, fmt.Errorf("parse pod list: %w", err)
	}

	var files []File
	for _, item := range podList.Items {
		name := item.Metadata.Name
		if name == "" {
			continue
		}
		logs, err := runOutput(ctx, "kubectl",
			append(kargs, "logs", name, "-n", wasNamespace,
				fmt.Sprintf("--tail=%d", maxLines), "--all-containers")...)
		if err != nil {
			logs = []byte(fmt.Sprintf("# error collecting logs for %s: %s\n", name, err))
		}
		// Sanitize log content.
		sanitized := redactText(string(logs))
		// Replace any JSON-structured log lines that may contain sensitive fields.
		sanitized = redactJSONLogLines(sanitized)
		safeName := strings.ReplaceAll(name, "/", "_")
		files = append(files, File{Path: "workloads/logs/" + safeName + ".log", Content: []byte(sanitized)})
	}

	return files, nil
}

// redactJSONLogLines walks each line; if it's a JSON object, redacts sensitive fields.
func redactJSONLogLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if len(line) < 2 || line[0] != '{' {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		var dummy []string
		cleaned := redactValue(obj, "", &dummy)
		if b, err := json.Marshal(cleaned); err == nil {
			lines[i] = string(b)
		}
	}
	return strings.Join(lines, "\n")
}
