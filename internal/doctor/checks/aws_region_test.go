package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func TestAWSRegion_Pass(t *testing.T) {
	orig := awsDescribeRegionFn
	defer func() { awsDescribeRegionFn = orig }()
	awsDescribeRegionFn = func(_ context.Context, region string) (string, error) {
		return region, nil
	}

	rc := &doctor.RunContext{Cfg: &config.Config{
		Region:     config.Field[string]{Value: "us-east-1"},
		MetaRegion: config.Field[string]{Value: "us-east-1"},
	}}
	f := AWSRegion{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAWSRegion_Fail_UnknownRegion(t *testing.T) {
	rc := &doctor.RunContext{Cfg: &config.Config{
		Region:     config.Field[string]{Value: "us-fake-99"},
		MetaRegion: config.Field[string]{Value: "us-east-1"},
	}}
	f := AWSRegion{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
}

func TestAWSRegion_PassOffline_OnlineFails(t *testing.T) {
	orig := awsDescribeRegionFn
	defer func() { awsDescribeRegionFn = orig }()
	awsDescribeRegionFn = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("network error")
	}

	rc := &doctor.RunContext{Cfg: &config.Config{
		Region:     config.Field[string]{Value: "eu-west-1"},
		MetaRegion: config.Field[string]{Value: "us-east-1"},
	}}
	// Offline check passes even if online call fails
	f := AWSRegion{}.Run(context.Background(), rc)
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass (offline), got %v: %s", f.Status, f.Message)
	}
}
