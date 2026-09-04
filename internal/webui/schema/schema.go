// Package schema defines the field descriptor types produced by
// cmd/values-schema-gen and consumed by the chart-only form renderer.
package schema

import (
	"encoding/json"
	"io/fs"
)

// Field describes one configurable value in values.yaml.
type Field struct {
	Path        string   `json:"path"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // "string","bool","int","hostname","text"
	Required    bool     `json:"required"`
	Description string   `json:"description,omitempty"`
	Default     string   `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Group       string   `json:"group,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"` // depth > 3 or complex type
}

// Schema is the root object in values.schema.json.
type Schema struct {
	Version string   `json:"version"`
	Groups  []string `json:"groups"`
	Fields  []Field  `json:"fields"`
}

// Load reads and parses the schema JSON from fsys at path.
func Load(fsys fs.FS, path string) (*Schema, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, err
	}
	var s Schema
	return &s, json.Unmarshal(data, &s)
}
