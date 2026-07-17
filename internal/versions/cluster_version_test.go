package versions

import (
	"strings"
	"testing"
)

func TestNormalizeK8sMinor(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{"1.36", "1.36", false},
		{"1.36.0", "1.36", false},
		{"v1.35", "1.35", false},
		{"1", "", true},
		{"", "", true},
		{"abc", "", true},
	}
	for _, tt := range tests {
		got, err := NormalizeK8sMinor(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("NormalizeK8sMinor(%q): want error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeK8sMinor(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeK8sMinor(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestK8sInstallChoices(t *testing.T) {
	got := K8sInstallChoices("1.36")
	want := []string{"1.36", "1.35", "1.34"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
	got = K8sInstallChoices("1.36.2")
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("patch normalize: got %v, want %v", got, want)
	}
	got = K8sInstallChoices("1.0")
	want = []string{"1.0"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("floor at 0: got %v, want %v", got, want)
	}
}

func TestDefaultClusterK8s(t *testing.T) {
	if got := DefaultClusterK8s("aws"); got != AWSClusterK8sDefault {
		t.Errorf("aws: got %q", got)
	}
	if got := DefaultClusterK8s("azure"); got != AzureClusterK8sDefault {
		t.Errorf("azure: got %q", got)
	}
	if got := DefaultClusterK8s(""); got != AWSClusterK8sDefault {
		t.Errorf("empty: got %q", got)
	}
}

func TestExtractClusterVersionDefault(t *testing.T) {
	tf := `
variable "cluster_name" {
  default = "was"
}
variable "cluster_version" {
  type        = string
  default     = "1.40"
  description = "test"
}
variable "other" {
  default = "1.99"
}
`
	got, err := ExtractClusterVersionDefault([]byte(tf))
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.40" {
		t.Fatalf("got %q, want 1.40", got)
	}

	_, err = ExtractClusterVersionDefault([]byte(`variable "x" { default = "1.2" }`))
	if err == nil {
		t.Fatal("expected error when cluster_version missing")
	}
}

func TestSupportedK8sVersionPrefixes(t *testing.T) {
	prefs := SupportedK8sVersionPrefixes()
	if len(prefs) == 0 {
		t.Fatal("empty prefixes")
	}
	// Matrix min is 1.30; cloud defaults are 1.36 → must include both ends.
	wantHave := []string{"1.30.", "1.36."}
	join := strings.Join(prefs, " ")
	for _, w := range wantHave {
		found := false
		for _, p := range prefs {
			if p == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q in %s", w, join)
		}
	}
	// Should not extend past max default.
	for _, p := range prefs {
		if p == "1.37." {
			t.Errorf("unexpected %q beyond cloud defaults", p)
		}
	}
}
