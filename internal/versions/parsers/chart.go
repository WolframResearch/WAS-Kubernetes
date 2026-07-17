package parsers

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var chartVersionFn = func(_ context.Context) ([]byte, error) {
	return os.ReadFile("charts/wolfram-application-server/Chart.yaml")
}

// chartVersionRE matches the top-level `version:` field in a Chart.yaml.
// Chart.yaml uses strict YAML where the version line has no leading whitespace.
var chartVersionRE = regexp.MustCompile(`(?m)^version:\s+(\S+)`)

// Chart returns the local WAS chart version by reading Chart.yaml.
// Returns an error if the file does not exist (pre-checkout or different CWD),
// which causes Check() to silently skip this component.
func Chart(ctx context.Context) (Version, error) {
	data, err := chartVersionFn(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("chart version: %w", err)
	}
	m := chartVersionRE.FindSubmatch(data)
	if m == nil {
		return Version{}, fmt.Errorf("chart version: 'version:' field not found in Chart.yaml")
	}
	raw := strings.TrimSpace(string(m[1]))
	v, err := Parse(raw)
	if err != nil {
		return Version{}, fmt.Errorf("chart version parse %q: %w", raw, err)
	}
	return v, nil
}
