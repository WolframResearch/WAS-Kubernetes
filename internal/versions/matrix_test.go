package versions

import (
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/versions/parsers"
)

func mustParse(t *testing.T, s string) Version {
	t.Helper()
	v, err := parsers.Parse(s)
	if err != nil {
		t.Fatalf("mustParse(%q): %v", s, err)
	}
	return v
}

func TestVersionRange_InRange(t *testing.T) {
	r := mustRange("1.28.0", "1.35.99")
	cases := []struct {
		ver  string
		want bool
	}{
		{"1.28.0", true},  // exact min
		{"1.35.99", true}, // exact max
		{"1.30.0", true},  // middle
		{"1.27.9", false}, // below min
		{"1.36.0", false}, // above max
		// pre-release below min is Critical: v1.28.0-rc.1 < v1.28.0
		{"v1.28.0-rc.1", false},
	}
	for _, tc := range cases {
		v := mustParse(t, tc.ver)
		if got := r.InRange(v); got != tc.want {
			t.Errorf("InRange(%q) = %v, want %v", tc.ver, got, tc.want)
		}
	}
}

func TestVersionRange_String(t *testing.T) {
	r := mustRange("3.10.0", "3.17.99")
	s := r.String()
	if !strings.Contains(s, "3.10.0") || !strings.Contains(s, "3.17.99") {
		t.Errorf("String() = %q, expected both bounds", s)
	}
}

func TestMustRange_Panic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for invalid version")
		}
	}()
	mustRange("not-a-version", "1.0.0")
}

func TestMustRange_PanicMax(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for invalid max version")
		}
	}()
	mustRange("1.0.0", "not-a-version")
}

func TestCurrentMatrix_Parseable(t *testing.T) {
	// Ensure CurrentMatrix can be constructed without panic (init-time check).
	// If mustRange panicked, the program would have already crashed.
	if CurrentMatrix.WasctlVersion == "" {
		t.Fatal("WasctlVersion should be set")
	}
	// Spot-check a few ranges are well-formed (Min <= Max).
	for _, tc := range []struct {
		name string
		r    VersionRange
	}{
		{"Helm", CurrentMatrix.Helm},
		{"Kubectl", CurrentMatrix.Kubectl},
		{"Terraform", CurrentMatrix.Terraform},
		{"AWSCLI", CurrentMatrix.AWSCLI},
		{"AzureCLI", CurrentMatrix.AzureCLI},
		{"Kubernetes", CurrentMatrix.Kubernetes},
		{"Strimzi", CurrentMatrix.Strimzi},
		{"Chart", CurrentMatrix.Chart},
		{"AWSProvider", CurrentMatrix.AWSProvider},
		{"AzureProvider", CurrentMatrix.AzureProvider},
	} {
		if tc.r.Min.GreaterThan(tc.r.Max) {
			t.Errorf("Matrix.%s: Min %s > Max %s", tc.name, tc.r.Min, tc.r.Max)
		}
	}
}
