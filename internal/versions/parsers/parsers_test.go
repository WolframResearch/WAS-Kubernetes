package parsers

import (
	"context"
	"errors"
	"os"
	"testing"
)

// ── Version parsing ───────────────────────────────────────────────────────────

func TestParse_Standard(t *testing.T) {
	cases := []struct {
		in                    string
		major, minor, patch   int
		suffix                string
	}{
		{"v3.17.2", 3, 17, 2, ""},
		{"1.30.0", 1, 30, 0, ""},
		{"v1.30.0-rc.1", 1, 30, 0, "rc.1"},
		{"v1.9.5+build", 1, 9, 5, ""},        // build metadata stripped by regex
		{"0.43.0", 0, 43, 0, ""},
		{"v2.15.0", 2, 15, 0, ""},
	}
	for _, tc := range cases {
		v, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.in, err)
		}
		if v.Major != tc.major || v.Minor != tc.minor || v.Patch != tc.patch {
			t.Errorf("Parse(%q) = %d.%d.%d, want %d.%d.%d",
				tc.in, v.Major, v.Minor, v.Patch, tc.major, tc.minor, tc.patch)
		}
		if v.Suffix != tc.suffix {
			t.Errorf("Parse(%q) suffix = %q, want %q", tc.in, v.Suffix, tc.suffix)
		}
	}
}

func TestParse_Error(t *testing.T) {
	_, err := Parse("not-a-version")
	if err == nil {
		t.Fatal("expected error for non-version string")
	}
}

func TestVersion_String(t *testing.T) {
	v := Version{Major: 3, Minor: 17, Patch: 2, Raw: "v3.17.2+build"}
	if v.String() != "v3.17.2+build" {
		t.Errorf("String() = %q", v.String())
	}
	v2 := Version{Major: 1, Minor: 30, Patch: 0, Suffix: "rc.1"}
	if v2.String() != "1.30.0-rc.1" {
		t.Errorf("String() = %q", v2.String())
	}
}

func TestVersion_IsZero(t *testing.T) {
	if !(Version{}).IsZero() {
		t.Fatal("expected zero version to report IsZero")
	}
	v, _ := Parse("1.0.0")
	if v.IsZero() {
		t.Fatal("non-zero version reports IsZero")
	}
}

func TestVersion_LessThan(t *testing.T) {
	cases := []struct {
		a, b string
		less bool
	}{
		{"1.0.0", "2.0.0", true},
		{"1.29.0", "1.30.0", true},
		{"1.30.0", "1.30.1", true},
		{"1.30.0", "1.30.0", false},   // equal
		{"2.0.0", "1.0.0", false},     // greater
		{"v1.30.0-rc.1", "v1.30.0", true}, // pre-release < release
		{"v1.30.0", "v1.30.0-rc.1", false},
	}
	for _, tc := range cases {
		a, _ := Parse(tc.a)
		b, _ := Parse(tc.b)
		if got := a.LessThan(b); got != tc.less {
			t.Errorf("%s < %s: got %v, want %v", tc.a, tc.b, got, tc.less)
		}
	}
}

func TestVersion_GreaterThan(t *testing.T) {
	a, _ := Parse("2.0.0")
	b, _ := Parse("1.0.0")
	if !a.GreaterThan(b) {
		t.Fatal("2.0.0 should be > 1.0.0")
	}
	if b.GreaterThan(a) {
		t.Fatal("1.0.0 should not be > 2.0.0")
	}
}

// ── Helm ─────────────────────────────────────────────────────────────────────

func TestHelm_Pass(t *testing.T) {
	data, _ := os.ReadFile("testdata/helm_version.txt")
	orig := helmVersionFn
	defer func() { helmVersionFn = orig }()
	helmVersionFn = func(_ context.Context) ([]byte, error) { return data, nil }

	v, err := Helm(context.Background())
	if err != nil {
		t.Fatalf("Helm: %v", err)
	}
	if v.Major != 3 || v.Minor != 17 || v.Patch != 2 {
		t.Errorf("Helm version = %v, want 3.17.2", v)
	}
}

func TestHelm_Error(t *testing.T) {
	orig := helmVersionFn
	defer func() { helmVersionFn = orig }()
	helmVersionFn = func(_ context.Context) ([]byte, error) {
		return nil, errors.New("not found")
	}
	if _, err := Helm(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestHelm_BadOutput(t *testing.T) {
	orig := helmVersionFn
	defer func() { helmVersionFn = orig }()
	helmVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte("not-a-version\n"), nil
	}
	if _, err := Helm(context.Background()); err == nil {
		t.Fatal("expected error for bad output")
	}
}

// ── Kubectl ───────────────────────────────────────────────────────────────────

func TestKubectl_Pass(t *testing.T) {
	data, _ := os.ReadFile("testdata/kubectl_version.json")
	orig := kubectlVersionFn
	defer func() { kubectlVersionFn = orig }()
	kubectlVersionFn = func(_ context.Context) ([]byte, error) { return data, nil }

	v, err := Kubectl(context.Background())
	if err != nil {
		t.Fatalf("Kubectl: %v", err)
	}
	if v.Major != 1 || v.Minor != 30 || v.Patch != 2 {
		t.Errorf("Kubectl version = %v, want 1.30.2", v)
	}
}

func TestKubectl_Error(t *testing.T) {
	orig := kubectlVersionFn
	defer func() { kubectlVersionFn = orig }()
	kubectlVersionFn = func(_ context.Context) ([]byte, error) {
		return nil, errors.New("not found")
	}
	if _, err := Kubectl(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestKubectl_BadJSON(t *testing.T) {
	orig := kubectlVersionFn
	defer func() { kubectlVersionFn = orig }()
	kubectlVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte("not json"), nil
	}
	if _, err := Kubectl(context.Background()); err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

// ── Terraform ─────────────────────────────────────────────────────────────────

func TestTerraform_Pass(t *testing.T) {
	data, _ := os.ReadFile("testdata/terraform_version.json")
	orig := terraformVersionFn
	defer func() { terraformVersionFn = orig }()
	terraformVersionFn = func(_ context.Context) ([]byte, error) { return data, nil }

	v, err := Terraform(context.Background())
	if err != nil {
		t.Fatalf("Terraform: %v", err)
	}
	if v.Major != 1 || v.Minor != 9 || v.Patch != 5 {
		t.Errorf("Terraform version = %v, want 1.9.5", v)
	}
}

func TestTerraform_Error(t *testing.T) {
	orig := terraformVersionFn
	defer func() { terraformVersionFn = orig }()
	terraformVersionFn = func(_ context.Context) ([]byte, error) {
		return nil, errors.New("not found")
	}
	if _, err := Terraform(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestTerraform_BadJSON(t *testing.T) {
	orig := terraformVersionFn
	defer func() { terraformVersionFn = orig }()
	terraformVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte("{invalid json}"), nil
	}
	if _, err := Terraform(context.Background()); err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

// ── AWS ───────────────────────────────────────────────────────────────────────

func TestAWS_Pass(t *testing.T) {
	data, _ := os.ReadFile("testdata/aws_version.txt")
	orig := awsVersionFn
	defer func() { awsVersionFn = orig }()
	awsVersionFn = func(_ context.Context) ([]byte, error) { return data, nil }

	v, err := AWS(context.Background())
	if err != nil {
		t.Fatalf("AWS: %v", err)
	}
	if v.Major != 2 || v.Minor != 15 || v.Patch != 0 {
		t.Errorf("AWS version = %v, want 2.15.0", v)
	}
}

func TestAWS_Error(t *testing.T) {
	orig := awsVersionFn
	defer func() { awsVersionFn = orig }()
	awsVersionFn = func(_ context.Context) ([]byte, error) {
		return nil, errors.New("not found")
	}
	if _, err := AWS(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestAWS_BadOutput(t *testing.T) {
	orig := awsVersionFn
	defer func() { awsVersionFn = orig }()
	awsVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte("unknown tool v1"), nil
	}
	if _, err := AWS(context.Background()); err == nil {
		t.Fatal("expected error for missing aws-cli/ prefix")
	}
}

// ── AzureCLI ──────────────────────────────────────────────────────────────────

func TestAzureCLI_Pass(t *testing.T) {
	data, _ := os.ReadFile("testdata/az_version.json")
	orig := azVersionFn
	defer func() { azVersionFn = orig }()
	azVersionFn = func(_ context.Context) ([]byte, error) { return data, nil }

	v, err := AzureCLI(context.Background())
	if err != nil {
		t.Fatalf("AzureCLI: %v", err)
	}
	if v.Major != 2 || v.Minor != 60 || v.Patch != 0 {
		t.Errorf("AzureCLI version = %v, want 2.60.0", v)
	}
}

func TestAzureCLI_Error(t *testing.T) {
	orig := azVersionFn
	defer func() { azVersionFn = orig }()
	azVersionFn = func(_ context.Context) ([]byte, error) {
		return nil, errors.New("not found")
	}
	if _, err := AzureCLI(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestAzureCLI_MissingField(t *testing.T) {
	orig := azVersionFn
	defer func() { azVersionFn = orig }()
	azVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte(`{"other": "2.0.0"}`), nil
	}
	if _, err := AzureCLI(context.Background()); err == nil {
		t.Fatal("expected error when azure-cli field missing")
	}
}

func TestAzureCLI_BadJSON(t *testing.T) {
	orig := azVersionFn
	defer func() { azVersionFn = orig }()
	azVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte("not json"), nil
	}
	if _, err := AzureCLI(context.Background()); err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

// ── Kubernetes (server) ───────────────────────────────────────────────────────

func TestKubernetes_Pass(t *testing.T) {
	data, _ := os.ReadFile("testdata/kubernetes_version.json")
	orig := kubernetesVersionFn
	defer func() { kubernetesVersionFn = orig }()
	kubernetesVersionFn = func(_ context.Context) ([]byte, error) { return data, nil }

	v, err := Kubernetes(context.Background())
	if err != nil {
		t.Fatalf("Kubernetes: %v", err)
	}
	if v.Major != 1 || v.Minor != 31 || v.Patch != 2 {
		t.Errorf("Kubernetes version = %v, want 1.31.2", v)
	}
}

func TestKubernetes_Error(t *testing.T) {
	orig := kubernetesVersionFn
	defer func() { kubernetesVersionFn = orig }()
	kubernetesVersionFn = func(_ context.Context) ([]byte, error) {
		return nil, errors.New("connection refused")
	}
	if _, err := Kubernetes(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestKubernetes_NoServerVersion(t *testing.T) {
	orig := kubernetesVersionFn
	defer func() { kubernetesVersionFn = orig }()
	kubernetesVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte(`{"clientVersion":{"gitVersion":"v1.30.2"}}`), nil
	}
	if _, err := Kubernetes(context.Background()); err == nil {
		t.Fatal("expected error when serverVersion missing")
	}
}

func TestKubernetes_BadJSON(t *testing.T) {
	orig := kubernetesVersionFn
	defer func() { kubernetesVersionFn = orig }()
	kubernetesVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte("not json"), nil
	}
	if _, err := Kubernetes(context.Background()); err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

// ── Strimzi ───────────────────────────────────────────────────────────────────

func TestStrimzi_Pass(t *testing.T) {
	data, _ := os.ReadFile("testdata/strimzi_deployment.json")
	orig := strimziVersionFn
	defer func() { strimziVersionFn = orig }()
	strimziVersionFn = func(_ context.Context) ([]byte, error) { return data, nil }

	v, err := Strimzi(context.Background())
	if err != nil {
		t.Fatalf("Strimzi: %v", err)
	}
	if v.Major != 0 || v.Minor != 43 || v.Patch != 0 {
		t.Errorf("Strimzi version = %v, want 0.43.0", v)
	}
}

func TestStrimzi_Error(t *testing.T) {
	orig := strimziVersionFn
	defer func() { strimziVersionFn = orig }()
	strimziVersionFn = func(_ context.Context) ([]byte, error) {
		return nil, errors.New("not found")
	}
	if _, err := Strimzi(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestStrimzi_NoContainers(t *testing.T) {
	orig := strimziVersionFn
	defer func() { strimziVersionFn = orig }()
	strimziVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte(`{"spec":{"template":{"spec":{"containers":[]}}}}`), nil
	}
	if _, err := Strimzi(context.Background()); err == nil {
		t.Fatal("expected error for empty containers")
	}
}

func TestStrimzi_NoTag(t *testing.T) {
	orig := strimziVersionFn
	defer func() { strimziVersionFn = orig }()
	strimziVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte(`{"spec":{"template":{"spec":{"containers":[{"image":"quay.io/strimzi/operator"}]}}}}`), nil
	}
	if _, err := Strimzi(context.Background()); err == nil {
		t.Fatal("expected error for image without tag")
	}
}

func TestStrimzi_BadJSON(t *testing.T) {
	orig := strimziVersionFn
	defer func() { strimziVersionFn = orig }()
	strimziVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte("not json"), nil
	}
	if _, err := Strimzi(context.Background()); err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

// ── Chart ─────────────────────────────────────────────────────────────────────

func TestChart_Pass(t *testing.T) {
	data, _ := os.ReadFile("testdata/chart.yaml")
	orig := chartVersionFn
	defer func() { chartVersionFn = orig }()
	chartVersionFn = func(_ context.Context) ([]byte, error) { return data, nil }

	v, err := Chart(context.Background())
	if err != nil {
		t.Fatalf("Chart: %v", err)
	}
	if v.Major != 1 || v.Minor != 0 || v.Patch != 0 {
		t.Errorf("Chart version = %v, want 1.0.0", v)
	}
}

func TestChart_Error(t *testing.T) {
	orig := chartVersionFn
	defer func() { chartVersionFn = orig }()
	chartVersionFn = func(_ context.Context) ([]byte, error) {
		return nil, errors.New("file not found")
	}
	if _, err := Chart(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestChart_MissingVersionField(t *testing.T) {
	orig := chartVersionFn
	defer func() { chartVersionFn = orig }()
	chartVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte("apiVersion: v2\nname: test\n"), nil
	}
	if _, err := Chart(context.Background()); err == nil {
		t.Fatal("expected error for missing version field")
	}
}

func TestChart_BadVersion(t *testing.T) {
	orig := chartVersionFn
	defer func() { chartVersionFn = orig }()
	chartVersionFn = func(_ context.Context) ([]byte, error) {
		return []byte("version: not-a-version\n"), nil
	}
	if _, err := Chart(context.Background()); err == nil {
		t.Fatal("expected error for unparseable version")
	}
}

// ── TerraformAWSProvider ──────────────────────────────────────────────────────

func TestTerraformAWSProvider_Pass(t *testing.T) {
	data, _ := os.ReadFile("testdata/terraform_version.json")
	orig := terraformProvidersOutputFn
	defer func() { terraformProvidersOutputFn = orig }()
	terraformProvidersOutputFn = func(_ context.Context) ([]byte, error) { return data, nil }

	v, err := TerraformAWSProvider(context.Background())
	if err != nil {
		t.Fatalf("TerraformAWSProvider: %v", err)
	}
	if v.Major != 5 || v.Minor != 60 || v.Patch != 0 {
		t.Errorf("TerraformAWSProvider version = %v, want 5.60.0", v)
	}
}

func TestTerraformAWSProvider_Error(t *testing.T) {
	orig := terraformProvidersOutputFn
	defer func() { terraformProvidersOutputFn = orig }()
	terraformProvidersOutputFn = func(_ context.Context) ([]byte, error) {
		return nil, errors.New("not found")
	}
	if _, err := TerraformAWSProvider(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestTerraformAWSProvider_NotInSelections(t *testing.T) {
	orig := terraformProvidersOutputFn
	defer func() { terraformProvidersOutputFn = orig }()
	terraformProvidersOutputFn = func(_ context.Context) ([]byte, error) {
		return []byte(`{"provider_selections":{}}`), nil
	}
	if _, err := TerraformAWSProvider(context.Background()); err == nil {
		t.Fatal("expected error when provider not in selections")
	}
}

func TestTerraformAWSProvider_BadJSON(t *testing.T) {
	orig := terraformProvidersOutputFn
	defer func() { terraformProvidersOutputFn = orig }()
	terraformProvidersOutputFn = func(_ context.Context) ([]byte, error) {
		return []byte("not json"), nil
	}
	if _, err := TerraformAWSProvider(context.Background()); err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

// ── TerraformAzureProvider ────────────────────────────────────────────────────

func TestTerraformAzureProvider_Pass(t *testing.T) {
	data, _ := os.ReadFile("testdata/terraform_version.json")
	orig := terraformProvidersOutputFn
	defer func() { terraformProvidersOutputFn = orig }()
	terraformProvidersOutputFn = func(_ context.Context) ([]byte, error) { return data, nil }

	v, err := TerraformAzureProvider(context.Background())
	if err != nil {
		t.Fatalf("TerraformAzureProvider: %v", err)
	}
	if v.Major != 3 || v.Minor != 95 || v.Patch != 0 {
		t.Errorf("TerraformAzureProvider version = %v, want 3.95.0", v)
	}
}

func TestTerraformAzureProvider_NotInSelections(t *testing.T) {
	orig := terraformProvidersOutputFn
	defer func() { terraformProvidersOutputFn = orig }()
	terraformProvidersOutputFn = func(_ context.Context) ([]byte, error) {
		return []byte(`{"provider_selections":{"registry.terraform.io/hashicorp/aws":"5.60.0"}}`), nil
	}
	if _, err := TerraformAzureProvider(context.Background()); err == nil {
		t.Fatal("expected error when azure provider not in selections")
	}
}
