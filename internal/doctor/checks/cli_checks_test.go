package checks

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func TestCLIHelm_Pass(t *testing.T) {
	origLook := lookPath
	origVer := cliVersionOutputFn
	defer func() { lookPath = origLook; cliVersionOutputFn = origVer }()

	lookPath = func(s string) (string, error) { return "/usr/bin/" + s, nil }
	cliVersionOutputFn = func(_ context.Context, bin string, _ []string) (string, error) {
		return "v3.14.0", nil
	}

	rc := &doctor.RunContext{Cfg: testCfg()}
	f := CLIHelm.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestCLIHelm_Fail_NotFound(t *testing.T) {
	origLook := lookPath
	defer func() { lookPath = origLook }()
	lookPath = func(s string) (string, error) { return "", errors.New("not found") }

	rc := &doctor.RunContext{Cfg: testCfg()}
	f := CLIHelm.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
}

func TestCLIHelm_Fail_OldVersion(t *testing.T) {
	origLook := lookPath
	origVer := cliVersionOutputFn
	defer func() { lookPath = origLook; cliVersionOutputFn = origVer }()

	lookPath = func(s string) (string, error) { return "/usr/bin/" + s, nil }
	cliVersionOutputFn = func(_ context.Context, _ string, _ []string) (string, error) {
		return "v2.17.0", nil
	}

	rc := &doctor.RunContext{Cfg: testCfg()}
	f := CLIHelm.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail for helm v2, got %v: %s", f.Status, f.Message)
	}
}

func TestCLIAz_Skip_AWS(t *testing.T) {
	rc := &doctor.RunContext{Cfg: &config.Config{
		Cloud:      "aws",
		MetaRegion: config.Field[string]{Value: "us-east-1"},
		Region:     config.Field[string]{Value: "us-east-1"},
	}}
	skip, _ := CLIAz.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected az check to be skipped for AWS cloud")
	}
}

func TestCLIKubelogin_Skip_AWS(t *testing.T) {
	rc := &doctor.RunContext{Cfg: &config.Config{
		Cloud:      "aws",
		MetaRegion: config.Field[string]{Value: "us-east-1"},
		Region:     config.Field[string]{Value: "us-east-1"},
	}}
	skip, _ := CLIKubelogin.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected kubelogin check to be skipped for AWS cloud")
	}
}

func TestCLIKubelogin_Fail_NotFound(t *testing.T) {
	origLook := lookPath
	defer func() { lookPath = origLook }()
	lookPath = func(s string) (string, error) { return "", errors.New("not found") }

	rc := &doctor.RunContext{Cfg: &config.Config{Cloud: "azure"}}
	f := CLIKubelogin.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
	if !strings.Contains(f.Remediation, "optional") && !strings.Contains(strings.ToLower(f.Remediation), "--admin") {
		t.Fatalf("expected optional/--admin remediation, got %q", f.Remediation)
	}
}

func TestCLITerraform_Pass(t *testing.T) {
	origLook := lookPath
	origVer := cliVersionOutputFn
	defer func() { lookPath = origLook; cliVersionOutputFn = origVer }()

	lookPath = func(s string) (string, error) { return "/usr/bin/" + s, nil }
	cliVersionOutputFn = func(_ context.Context, _ string, _ []string) (string, error) {
		return `{"terraform_version":"1.8.0"}`, nil
	}

	rc := &doctor.RunContext{Cfg: testCfg()}
	f := CLITerraform.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}
