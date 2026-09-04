package versions

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const k8sInstallWindow = 3 // N, N-1, N-2

// majorMinorRE matches Kubernetes install pins like "1.36" or "1.36.0".
var majorMinorRE = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.\d+)?$`)

// clusterVersionDefaultRE finds default = "X.Y" inside variable "cluster_version" { … }.
var clusterVersionDefaultRE = regexp.MustCompile(
	`(?s)variable\s+"cluster_version"\s*\{.*?default\s*=\s*"([^"]+)"`,
)

// ExtractClusterVersionDefault parses a Terraform variables.tf body and returns
// the default value of variable "cluster_version" as major.minor.
func ExtractClusterVersionDefault(tf []byte) (string, error) {
	m := clusterVersionDefaultRE.FindSubmatch(tf)
	if m == nil {
		return "", fmt.Errorf(`variable "cluster_version" default not found`)
	}
	minor, err := NormalizeK8sMinor(string(m[1]))
	if err != nil {
		return "", fmt.Errorf("cluster_version default %q: %w", m[1], err)
	}
	return minor, nil
}

// ExtractClusterVersionDefaultFile reads path and returns its cluster_version default.
func ExtractClusterVersionDefaultFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return ExtractClusterVersionDefault(b)
}

// NormalizeK8sMinor accepts "1.36" or "1.36.0" and returns "1.36".
func NormalizeK8sMinor(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	m := majorMinorRE.FindStringSubmatch(s)
	if m == nil {
		return "", fmt.Errorf("want major.minor (optionally .patch), got %q", s)
	}
	return m[1] + "." + m[2], nil
}

// parseMajorMinor returns major and minor ints for a normalized or raw pin.
func parseMajorMinor(s string) (major, minor int, err error) {
	n, err := NormalizeK8sMinor(s)
	if err != nil {
		return 0, 0, err
	}
	parts := strings.SplitN(n, ".", 2)
	major, _ = strconv.Atoi(parts[0])
	minor, _ = strconv.Atoi(parts[1])
	return major, minor, nil
}

// DefaultClusterK8s returns the install default for cloud ("aws" or "azure").
// Unknown clouds fall back to AWS.
func DefaultClusterK8s(cloud string) string {
	if strings.EqualFold(cloud, "azure") {
		return AzureClusterK8sDefault
	}
	return AWSClusterK8sDefault
}

// K8sInstallChoices returns the last k8sInstallWindow minors ending at defaultMinor
// (newest first): for "1.36" → ["1.36","1.35","1.34"].
func K8sInstallChoices(defaultMinor string) []string {
	major, minor, err := parseMajorMinor(defaultMinor)
	if err != nil {
		return []string{defaultMinor}
	}
	out := make([]string, 0, k8sInstallWindow)
	for i := 0; i < k8sInstallWindow; i++ {
		m := minor - i
		if m < 0 {
			break
		}
		out = append(out, fmt.Sprintf("%d.%d", major, m))
	}
	return out
}

// SupportedK8sVersionPrefixes builds doctor allowlist prefixes from the matrix
// Kubernetes min major.minor through max(AWS, Azure) cluster defaults inclusive.
func SupportedK8sVersionPrefixes() []string {
	minMajor := CurrentMatrix.Kubernetes.Min.Major
	minMinor := CurrentMatrix.Kubernetes.Min.Minor

	maxS := AWSClusterK8sDefault
	if cmpMinor(AzureClusterK8sDefault, maxS) > 0 {
		maxS = AzureClusterK8sDefault
	}
	maxMajor, maxMinor, err := parseMajorMinor(maxS)
	if err != nil {
		// Fall back to matrix max if generated defaults are somehow broken.
		maxMajor = CurrentMatrix.Kubernetes.Max.Major
		maxMinor = CurrentMatrix.Kubernetes.Max.Minor
	}

	var out []string
	for maj := minMajor; maj <= maxMajor; maj++ {
		start := 0
		end := 99
		if maj == minMajor {
			start = minMinor
		}
		if maj == maxMajor {
			end = maxMinor
		}
		for min := start; min <= end; min++ {
			out = append(out, fmt.Sprintf("%d.%d.", maj, min))
		}
	}
	return out
}

// cmpMinor compares two major.minor pins: -1, 0, or 1.
func cmpMinor(a, b string) int {
	am, an, aerr := parseMajorMinor(a)
	bm, bn, berr := parseMajorMinor(b)
	if aerr != nil || berr != nil {
		return strings.Compare(a, b)
	}
	if am != bm {
		if am < bm {
			return -1
		}
		return 1
	}
	if an < bn {
		return -1
	}
	if an > bn {
		return 1
	}
	return 0
}
