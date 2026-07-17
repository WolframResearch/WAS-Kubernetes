package supportbundle

import (
	"encoding/json"
	"time"
)

// ManifestVersion identifies the manifest schema. Increment on breaking changes.
const ManifestVersion = "1"

// Manifest is written as manifest.json at the bundle root. It records what's
// inside the bundle, what sanitization was applied, and bundle metadata.
type Manifest struct {
	Version             string    `json:"version"`
	GeneratedAt         time.Time `json:"generatedAt"`
	WasctlVersion       string    `json:"wasctlVersion"`
	SanitizationVersion string    `json:"sanitizationVersion"`
	Cluster             string    `json:"cluster"`
	Sections            []string  `json:"sections"`
	Redactions          []string  `json:"redactions,omitempty"`
}

// JSON returns the manifest serialized as indented JSON.
func (m *Manifest) JSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}
