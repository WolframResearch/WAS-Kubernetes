// Package parsers provides version detection for tools used by wasctl.
// Each parser runs the tool's version command and returns a structured Version.
// All parser functions are injectable for testing via package-level function variables.
package parsers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is the canonical structured representation of a semver-like version.
type Version struct {
	Major, Minor, Patch int
	// Suffix holds pre-release labels (e.g. "rc.1", "alpha", "beta.2").
	// A version with a non-empty Suffix is considered LESS THAN the same
	// Major.Minor.Patch with no Suffix. This is the most conservative semantic:
	// v1.30.0-rc.1 is below the range min of 1.30.0 and is Critical.
	Suffix string
	Raw    string // original unparsed string
}

// versionRE matches forms like: v1.2.3, 1.2.3, v1.2.3-rc.1, 1.2.3+build.
var versionRE = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)(?:[.\-]([a-zA-Z][^+\s]*))?`)

// Parse parses a semver-like string into a Version. Returns an error if no
// version pattern can be found.
func Parse(s string) (Version, error) {
	s = strings.TrimSpace(s)
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return Version{Raw: s}, fmt.Errorf("cannot parse version from %q", s)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return Version{Major: major, Minor: minor, Patch: patch, Suffix: m[4], Raw: s}, nil
}

// String returns a canonical string for this version.
func (v Version) String() string {
	if v.Raw != "" {
		return v.Raw
	}
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Suffix != "" {
		s += "-" + v.Suffix
	}
	return s
}

// IsZero reports whether the Version is the zero value (unpopulated).
func (v Version) IsZero() bool {
	return v.Major == 0 && v.Minor == 0 && v.Patch == 0 && v.Suffix == "" && v.Raw == ""
}

// LessThan returns true if v < other. Pre-releases are considered less than
// their release counterpart: v1.30.0-rc.1 < v1.30.0.
func (v Version) LessThan(other Version) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}
	if v.Patch != other.Patch {
		return v.Patch < other.Patch
	}
	// Same Major.Minor.Patch: pre-release < release.
	if v.Suffix != "" && other.Suffix == "" {
		return true
	}
	if v.Suffix == "" && other.Suffix != "" {
		return false
	}
	// Both have suffixes or neither does: compare lexicographically.
	return v.Suffix < other.Suffix
}

// GreaterThan returns true if v > other.
func (v Version) GreaterThan(other Version) bool {
	return other.LessThan(v)
}
