// Package versions validates installed tool versions against the wasctl
// compatibility matrix and reports incompatibilities as typed Issues.
//
// The matrix is defined in matrix.go (CurrentMatrix). The parsers sub-package
// provides per-tool version detection. check.go wires them together.
package versions
