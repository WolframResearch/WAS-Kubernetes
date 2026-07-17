package stages

// Backend drift test: ensures BackendHCLContent output stays in sync with the
// backend_hcl_hint output in infra/aws/bootstrap/outputs.tf. If wasctl writes
// one thing and the TF hint says another, an operator copy-pasting the hint
// into a backend.hcl will get a mismatch. This test fails the build if they
// ever diverge.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns the repo root by walking up from this test file's directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile is internal/stages/backend_drift_test.go
	// walk up 3 dirs: stages → internal → repo root
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("could not locate repo root (no go.mod at %s): %v", root, err)
	}
	return root
}

// TestBackendHCLDrift fails if BackendHCLContent output diverges from the
// backend_hcl_hint defined in infra/aws/bootstrap/outputs.tf.
func TestBackendHCLDrift(t *testing.T) {
	tfPath := filepath.Join(repoRoot(t), "infra", "aws", "bootstrap", "outputs.tf")
	src, err := os.ReadFile(tfPath)
	if err != nil {
		t.Fatalf("read outputs.tf: %v", err)
	}

	// Extract the heredoc between <<-EOT and EOT inside backend_hcl_hint.
	// Pattern: everything between the <<-EOT and the closing EOT in the value block.
	re := regexp.MustCompile(`(?s)backend_hcl_hint.*?<<-EOT\s*\n(.*?)\s*EOT`)
	m := re.FindSubmatch(src)
	if m == nil {
		t.Fatal("could not find backend_hcl_hint heredoc in outputs.tf — update this test if the output was renamed")
	}
	hintRaw := string(m[1])

	// The heredoc uses <<- which strips leading whitespace per-line (Terraform).
	// Strip the minimum indent from each line (mimics <<- behaviour).
	lines := strings.Split(hintRaw, "\n")
	minIndent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " \t"))
		if minIndent < 0 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent < 0 {
		minIndent = 0
	}
	stripped := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(l) >= minIndent {
			stripped = append(stripped, l[minIndent:])
		} else {
			stripped = append(stripped, l)
		}
	}
	hintContent := strings.Join(stripped, "\n")

	// Replace TF interpolation placeholders with test values.
	const testBucket = "test-state-bucket-12345"
	const testRegion = "us-east-1"
	hintContent = strings.ReplaceAll(hintContent, "${aws_s3_bucket.tfstate.bucket}", testBucket)
	hintContent = strings.ReplaceAll(hintContent, "${var.aws_region}", testRegion)

	want := strings.TrimRight(hintContent, "\n") + "\n"
	got := BackendHCLContent(testBucket, testRegion)

	if got != want {
		t.Errorf("BackendHCLContent output diverged from outputs.tf backend_hcl_hint.\n"+
			"Update BackendHCLContent or outputs.tf to match.\n\n"+
			"BackendHCLContent output:\n%s\n\noutputs.tf hint (after substitution):\n%s",
			got, want)
	}
}
