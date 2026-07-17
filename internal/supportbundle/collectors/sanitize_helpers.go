package collectors

// sanitize_helpers.go provides text-redaction utilities for collectors.
// These mirror the functions in internal/supportbundle/sanitizer.go but live
// in the collectors package to avoid a circular import (supportbundle → collectors).

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	reAWSKeyCol      = regexp.MustCompile(`AKIA[A-Z0-9]{16}`)
	reBearerJWTCol   = regexp.MustCompile(`(?i)(Bearer\s+)(eyJ[A-Za-z0-9._\-]+)`)
	reGenericJWTCol  = regexp.MustCompile(`eyJ[A-Za-z0-9._\-]{50,}`)
	reAzureConnCol   = regexp.MustCompile(`DefaultEndpointsProtocol=[^\s"']+`)
	reSensitiveKVCol = regexp.MustCompile(`(?i)((?:password|secret|token|credential)[^=:\s]*)\s*[=:]\s*(\S{4,})`)
	reSensitiveKeyCol = regexp.MustCompile(`(?i)password|secret|token|credential|private.*key|cert`)
)

var allowedEnvVarsCol = map[string]bool{
	"PATH": true, "AWS_PROFILE": true, "AWS_REGION": true, "AWS_DEFAULT_REGION": true,
	"KUBECONFIG": true, "USER": true, "HOME": true, "SHELL": true,
	"TERM": true, "LANG": true, "LC_ALL": true,
}

func redactText(s string) string {
	s = reAWSKeyCol.ReplaceAllString(s, "[REDACTED_AWS_KEY]")
	s = reBearerJWTCol.ReplaceAllString(s, "${1}[REDACTED_JWT]")
	s = reGenericJWTCol.ReplaceAllString(s, "[REDACTED_JWT]")
	s = reAzureConnCol.ReplaceAllString(s, "[REDACTED]")
	s = reSensitiveKVCol.ReplaceAllStringFunc(s, func(m string) string {
		parts := reSensitiveKVCol.FindStringSubmatch(m)
		if len(parts) < 2 {
			return m
		}
		return parts[1] + "=[REDACTED]"
	})
	return s
}

func redactEnvVars(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if allowedEnvVarsCol[strings.ToUpper(k)] {
			out = append(out, e)
		}
	}
	return out
}

func redactHelmValues(data []byte) ([]byte, []string, error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return []byte(redactText(string(data))), nil, nil
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
			if reSensitiveKeyCol.MatchString(k) {
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

func maskAccountID(id string) string {
	if len(id) != 12 {
		return id
	}
	return id[:4] + "****" + id[8:]
}

func redactWorkspaceJSON(data []byte) ([]byte, error) {
	var v map[string]interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return []byte(redactText(string(data))), nil
	}
	if acc, ok := v["awsAccountID"].(string); ok {
		v["awsAccountID"] = maskAccountID(acc)
	}
	return json.MarshalIndent(v, "", "  ")
}
