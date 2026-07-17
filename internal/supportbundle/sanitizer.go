package supportbundle

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// SanitizationVersion identifies the redaction ruleset. Increment when rules change.
const SanitizationVersion = "1.0"

var (
	reAWSKey      = regexp.MustCompile(`AKIA[A-Z0-9]{16}`)
	reBearerJWT   = regexp.MustCompile(`(?i)(Bearer\s+)(eyJ[A-Za-z0-9._\-]+)`)
	reGenericJWT  = regexp.MustCompile(`eyJ[A-Za-z0-9._\-]{50,}`)
	reAzureConn   = regexp.MustCompile(`DefaultEndpointsProtocol=[^\s"']+`)
	reSensitiveKV = regexp.MustCompile(`(?i)((?:password|secret|token|credential)[^=:\s]*)\s*[=:]\s*(\S{4,})`)
	reSensitiveKey = regexp.MustCompile(`(?i)password|secret|token|credential|private.*key|cert`)
)

// allowedEnvVars is the safe-to-include allowlist for environment variables.
var allowedEnvVars = map[string]bool{
	"PATH":               true,
	"AWS_PROFILE":        true,
	"AWS_REGION":         true,
	"AWS_DEFAULT_REGION": true,
	"KUBECONFIG":         true,
	"USER":               true,
	"HOME":               true,
	"SHELL":              true,
	"TERM":               true,
	"LANG":               true,
	"LC_ALL":             true,
}

// RedactText applies all regex redaction patterns to arbitrary text.
func RedactText(s string) string {
	s = reAWSKey.ReplaceAllString(s, "[REDACTED_AWS_KEY]")
	s = reBearerJWT.ReplaceAllString(s, "${1}[REDACTED_JWT]")
	s = reGenericJWT.ReplaceAllString(s, "[REDACTED_JWT]")
	s = reAzureConn.ReplaceAllString(s, "[REDACTED]")
	s = reSensitiveKV.ReplaceAllStringFunc(s, func(m string) string {
		parts := reSensitiveKV.FindStringSubmatch(m)
		if len(parts) < 2 {
			return m
		}
		return parts[1] + "=[REDACTED]"
	})
	return s
}

// RedactEnvVars returns only safe environment variables from the provided list.
// Never includes AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN,
// TOKEN_* prefixes, or any key not in the allowlist.
func RedactEnvVars(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		k = strings.TrimSpace(k)
		if allowedEnvVars[strings.ToUpper(k)] {
			out = append(out, e)
		}
	}
	return out
}

// RedactHelmValues deep-walks JSON-encoded Helm values and redacts leaf values
// whose key names match the sensitive-key pattern. Returns redacted JSON and a
// list of dot-path keys that were redacted (for manifest.json).
func RedactHelmValues(data []byte) ([]byte, []string, error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		// Not valid JSON — fall back to text redaction.
		return []byte(RedactText(string(data))), nil, nil
	}
	var redacted []string
	out := redactValue(v, "", &redacted)
	b, err := json.MarshalIndent(out, "", "  ")
	return b, redacted, err
}

func redactValue(v interface{}, path string, redacted *[]string) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			if reSensitiveKey.MatchString(k) {
				if s, ok := val.(string); ok && len(s) >= 4 {
					out[k] = "[REDACTED]"
					*redacted = append(*redacted, childPath)
					continue
				}
			}
			out[k] = redactValue(val, childPath, redacted)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, item := range t {
			out[i] = redactValue(item, fmt.Sprintf("%s[%d]", path, i), redacted)
		}
		return out
	default:
		return v
	}
}

// MaskAccountID masks the middle 4 digits of a 12-digit AWS account ID.
// E.g., "123456789012" → "1234****9012"
func MaskAccountID(id string) string {
	if len(id) != 12 {
		return id
	}
	return id[:4] + "****" + id[8:]
}

// RedactWorkspaceJSON sanitizes workspace.json by masking awsAccountID.
func RedactWorkspaceJSON(data []byte) ([]byte, error) {
	var v map[string]interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return []byte(RedactText(string(data))), nil
	}
	if acc, ok := v["awsAccountID"].(string); ok {
		v["awsAccountID"] = MaskAccountID(acc)
	}
	return json.MarshalIndent(v, "", "  ")
}
