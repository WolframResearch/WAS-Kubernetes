package stages

import (
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
)

func TestWantIngressTLS_AzureDefault(t *testing.T) {
	cfg := &config.Config{Cloud: "azure"}
	if !wantIngressTLS(cfg) {
		t.Fatal("Azure should enable TLS when cert-manager is not skipped")
	}
}

func TestWantIngressTLS_AWSRequiresCustomHost(t *testing.T) {
	cfg := &config.Config{Cloud: "aws"}
	if wantIngressTLS(cfg) {
		t.Fatal("AWS without ingress host must not enable TLS")
	}
	cfg.IngressHost = config.Field[string]{Value: "xxx.us-east-1.elb.amazonaws.com"}
	if wantIngressTLS(cfg) {
		t.Fatal("AWS ELB hostname must not enable TLS")
	}
	cfg.IngressHost = config.Field[string]{Value: "was.example.com"}
	if !wantIngressTLS(cfg) {
		t.Fatal("AWS custom DNS should enable TLS when cert-manager not skipped")
	}
}

func TestWantIngressTLS_Skipped(t *testing.T) {
	cfg := &config.Config{
		Cloud:      "azure",
		AddonsSkip: config.Field[string]{Value: "cert-manager"},
	}
	if wantIngressTLS(cfg) {
		t.Fatal("wantIngressTLS must be false when cert-manager skipped")
	}
}

func TestEnsureAWSCertManagerSkip(t *testing.T) {
	cfg := &config.Config{Cloud: "aws"}
	rep := &tlsLogCapture{}
	ensureAWSCertManagerSkip(cfg, rep)
	if !strings.Contains(cfg.AddonsSkip.Value, "cert-manager") {
		t.Fatalf("expected cert-manager skip, got %q", cfg.AddonsSkip.Value)
	}
	if len(rep.lines) == 0 {
		t.Fatal("expected log lines explaining AWS skip")
	}

	cfg2 := &config.Config{
		Cloud:       "aws",
		IngressHost: config.Field[string]{Value: "was.example.com"},
	}
	ensureAWSCertManagerSkip(cfg2, &tlsLogCapture{})
	if strings.Contains(cfg2.AddonsSkip.Value, "cert-manager") {
		t.Fatalf("must not skip cert-manager with custom host, got %q", cfg2.AddonsSkip.Value)
	}

	cfg3 := &config.Config{Cloud: "azure"}
	ensureAWSCertManagerSkip(cfg3, &tlsLogCapture{})
	if cfg3.AddonsSkip.Value != "" {
		t.Fatalf("Azure must not auto-skip, got %q", cfg3.AddonsSkip.Value)
	}
}

type tlsLogCapture struct{ lines []string }

func (c *tlsLogCapture) SubstepStart(string) {}
func (c *tlsLogCapture) SubstepDone()         {}
func (c *tlsLogCapture) SubstepFail(error)    {}
func (c *tlsLogCapture) LogLine(s string)     { c.lines = append(c.lines, s) }
