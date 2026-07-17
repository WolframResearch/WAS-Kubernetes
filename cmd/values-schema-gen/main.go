// Command values-schema-gen parses charts/wolfram-application-server/values.yaml
// and emits a JSON field-descriptor schema consumed by the wasctl web UI.
//
// Usage: values-schema-gen <values.yaml> <output.json>
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type field struct {
	Path        string   `json:"path"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Description string   `json:"description,omitempty"`
	Default     string   `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Group       string   `json:"group,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
}

type schema struct {
	Version string   `json:"version"`
	Groups  []string `json:"groups"`
	Fields  []field  `json:"fields"`
}

// ── Known enums for specific paths ────────────────────────────────────────────

var knownEnums = map[string][]string{
	"cloud":                            {"aws", "azure"},
	"ingress.pathType":                 {"ImplementationSpecific", "Prefix", "Exact"},
	"objectStorage.auth.mode":          {"irsa", "workloadIdentity", "static"},
	"kafka.mode":                       {"builtin", "external"},
	"resourceManager.image.pullPolicy": {"IfNotPresent", "Always", "Never"},
	"endpointManager.image.pullPolicy": {"IfNotPresent", "Always", "Never"},
	"awes.image.pullPolicy":            {"IfNotPresent", "Always", "Never"},
}

// ── Human-readable label overrides ────────────────────────────────────────────

var labelOverrides = map[string]string{
	"replicaCount":         "Replica Count",
	"pullPolicy":           "Pull Policy",
	"className":            "Storage Class",
	"roleArn":              "IAM Role ARN",
	"azureClientId":        "Azure Client ID",
	"installCRDs":          "Install CRDs",
	"secretName":           "Secret Name",
	"bootstrapServers":     "Bootstrap Servers",
	"clusterName":          "Cluster Name",
	"protocolVersion":      "Protocol Version",
	"accountName":          "Account Name",
	"accessKey":            "Access Key",
	"secretKey":            "Secret Key",
	"nodefileBucket":       "Nodefile Bucket",
	"resourceBucket":       "Resource Bucket",
	"logsSize":             "Logs Volume Size",
	"pathType":             "Path Type",
	"initialDelaySeconds":  "Initial Delay (s)",
	"periodSeconds":        "Period (s)",
	"nameOverride":         "Name Override",
	"fullnameOverride":     "Full Name Override",
	"credentials":          "htpasswd Credentials",
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// ambiguousTerminals are terminal path segments so generic that the label must
// include the parent key to be meaningful in a form.
var ambiguousTerminals = map[string]bool{
	"enabled": true, "create": true, "name": true, "size": true,
	"class": true, "port": true, "mode": true, "credentials": true,
	"version": true,
}

func makeLabel(path string) string {
	parts := strings.Split(path, ".")
	last := parts[len(parts)-1]

	if override, ok := labelOverrides[last]; ok {
		// Only use the override if the terminal is unambiguous.
		if !ambiguousTerminals[last] {
			return override
		}
		// Fall through to prepend parent.
		_ = override
	}

	label := camelToWords(last)

	// Prepend the direct parent key when this terminal is too generic.
	if ambiguousTerminals[last] && len(parts) > 1 {
		parent := camelToWords(parts[len(parts)-2])
		label = parent + " " + label
	}
	return strings.Title(label)
}

func camelToWords(s string) string {
	var out []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out = append(out, ' ')
		}
		// Replace hyphens with spaces for kebab-case keys like "ingress-nginx"
		if r == '-' {
			out = append(out, ' ')
		} else {
			out = append(out, r)
		}
	}
	return strings.ToLower(string(out))
}

func inferType(path, value string) string {
	if _, ok := knownEnums[path]; ok {
		return "string"
	}
	switch value {
	case "true", "false":
		return "bool"
	}
	if _, err := strconv.Atoi(value); err == nil {
		return "int"
	}
	if strings.HasSuffix(path, ".host") || strings.HasSuffix(path, "Host") {
		return "hostname"
	}
	if strings.HasSuffix(path, ".credentials") {
		return "text"
	}
	return "string"
}

func stripQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func isSeparator(comment string) bool {
	return strings.Count(comment, "=") > 10
}

// cleanGroupName strips parenthetical suffixes, normalises em-dashes, and
// maps well-known verbose titles to short names.
func cleanGroupName(s string) string {
	if strings.Contains(s, "Helm Chart Values") {
		return "General"
	}
	// Strip " (...)" parenthetical suffix
	if i := strings.Index(s, " ("); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	// Strip " — ..." em-dash suffix
	for _, sep := range []string{" — ", " – ", " - "} {
		if i := strings.Index(s, sep); i > 0 {
			s = strings.TrimSpace(s[:i])
		}
	}
	overrides := map[string]string{
		"Active Web Elements Server": "AWES",
		"Cloud target":               "Cloud",
		"Storage":                    "Storage",
	}
	if v, ok := overrides[s]; ok {
		return v
	}
	return s
}

func analyzeComments(comments []string) (required bool, desc string) {
	var parts []string
	for _, c := range comments {
		c = strings.TrimSpace(c)
		if isSeparator(c) || c == "" {
			continue
		}
		if strings.HasPrefix(c, "REQUIRED") {
			required = true
			rest := strings.TrimLeft(strings.TrimPrefix(c, "REQUIRED"), " —-–")
			if rest != "" {
				parts = append(parts, rest)
			}
		} else if strings.HasPrefix(c, "OPTIONAL") {
			rest := strings.TrimLeft(strings.TrimPrefix(c, "OPTIONAL"), " —-–")
			if rest != "" && !strings.HasPrefix(rest, "every") {
				parts = append(parts, rest)
			}
		} else if !strings.HasPrefix(c, "CONDITIONALLY") &&
			!strings.HasPrefix(c, "RECOMMENDED") &&
			!strings.HasPrefix(c, "WARNING") &&
			!strings.HasPrefix(c, "Enable any") &&
			!strings.HasPrefix(c, "Run `") &&
			!strings.HasPrefix(c, "When ") &&
			!strings.HasPrefix(c, "when ") &&
			!strings.HasPrefix(c, "Subchart") &&
			!strings.HasPrefix(c, "Already") &&
			!strings.HasPrefix(c, "Example") {
			parts = append(parts, c)
		}
	}
	desc = strings.Join(parts, " ")
	if len(desc) > 180 {
		desc = desc[:177] + "..."
	}
	return
}

func buildPath(stack []string, level int) string {
	var parts []string
	for i := 0; i <= level && i < len(stack); i++ {
		if stack[i] != "" {
			parts = append(parts, stack[i])
		}
	}
	return strings.Join(parts, ".")
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: values-schema-gen <values.yaml> <output.json>")
		os.Exit(1)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer f.Close()

	type rawLine struct {
		indent  int
		kind    string // "empty", "comment", "key"
		key     string
		value   string
		comment string
	}

	var lines []rawLine
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		s := scanner.Text()
		trimmed := strings.TrimLeft(s, " ")
		indent := len(s) - len(trimmed)

		if trimmed == "" {
			lines = append(lines, rawLine{indent: indent, kind: "empty"})
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			comment := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			lines = append(lines, rawLine{indent: indent, kind: "comment", comment: comment})
			continue
		}
		// Skip array items (e.g. kernel pool entries)
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			lines = append(lines, rawLine{indent: indent, kind: "empty"})
			continue
		}
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx < 0 {
			lines = append(lines, rawLine{indent: indent, kind: "empty"})
			continue
		}
		key := strings.TrimSpace(trimmed[:colonIdx])
		value := strings.TrimSpace(trimmed[colonIdx+1:])
		// Strip inline comment (one or more spaces before #)
		if idx := strings.Index(value, " #"); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		lines = append(lines, rawLine{indent: indent, kind: "key", key: key, value: value})
	}

	var fields []field
	groupOrder := []string{"General"}
	seenGroups := map[string]bool{"General": true}

	pathStack := make([]string, 16)
	var pendingComments []string

	currentGroup := "General"

	type groupDetectState int
	const (
		gNormal groupDetectState = iota
		gSep1
		gSep1Name
	)
	gs := gNormal
	var pendingGroupName string

	inBlockScalar := false
	blockMinIndent := 0

	for _, line := range lines {
		if inBlockScalar {
			if line.kind != "empty" && line.indent >= blockMinIndent {
				continue
			}
			inBlockScalar = false
		}

		switch line.kind {
		case "empty":
			if gs != gSep1Name {
				pendingComments = nil
			}
			if gs == gNormal {
				gs = gNormal
			}

		case "comment":
			if isSeparator(line.comment) {
				switch gs {
				case gNormal, gSep1:
					gs = gSep1
					pendingComments = nil
				case gSep1Name:
					// Complete 3-line group header
					currentGroup = cleanGroupName(pendingGroupName)
					if !seenGroups[currentGroup] {
						seenGroups[currentGroup] = true
						groupOrder = append(groupOrder, currentGroup)
					}
					gs = gNormal
					pendingComments = nil
				}
				continue
			}
			if gs == gSep1 {
				pendingGroupName = line.comment
				gs = gSep1Name
				continue
			}
			gs = gNormal
			pendingComments = append(pendingComments, line.comment)

		case "key":
			gs = gNormal
			level := line.indent / 2
			if level >= len(pathStack) {
				level = len(pathStack) - 1
			}

			pathStack[level] = line.key
			for j := level + 1; j < len(pathStack); j++ {
				pathStack[j] = ""
			}

			// Block scalar
			if line.value == "|" || line.value == "|-" || line.value == ">-" || line.value == ">" {
				path := buildPath(pathStack, level)
				req, desc := analyzeComments(pendingComments)
				hidden := len(strings.Split(path, ".")) > 3
				fields = append(fields, field{
					Path:        path,
					Label:       makeLabel(path),
					Type:        "text",
					Required:    req,
					Description: desc,
					Group:       currentGroup,
					Hidden:      hidden,
				})
				inBlockScalar = true
				blockMinIndent = (level + 1) * 2
				pendingComments = nil
				continue
			}

			// Empty value → parent object key; reset comments (they belong to this section header)
			if line.value == "" || line.value == "{}" || line.value == "[]" {
				pendingComments = nil
				continue
			}

			// Scalar leaf
			path := buildPath(pathStack, level)
			req, desc := analyzeComments(pendingComments)
			defVal := stripQuotes(line.value)
			typ := inferType(path, defVal)
			enum := knownEnums[path]
			hidden := len(strings.Split(path, ".")) > 3

			fields = append(fields, field{
				Path:        path,
				Label:       makeLabel(path),
				Type:        typ,
				Required:    req,
				Description: desc,
				Default:     defVal,
				Enum:        enum,
				Group:       currentGroup,
				Hidden:      hidden,
			})
			pendingComments = nil
		}
	}

	s := schema{
		Version: "1",
		Groups:  groupOrder,
		Fields:  fields,
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		os.Exit(1)
	}

	out, err := os.Create(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "create output:", err)
		os.Exit(1)
	}
	defer out.Close()
	_, _ = out.Write(data)
	_, _ = out.Write([]byte("\n"))
	fmt.Fprintf(os.Stderr, "values-schema-gen: wrote %d fields to %s\n", len(fields), os.Args[2])
}
