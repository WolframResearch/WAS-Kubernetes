package handlers

import (
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
)

func TestClusterDisplayStatus(t *testing.T) {
	tests := []struct {
		name string
		m    *metabucket.Metadata
		want string
	}{
		{name: "nil", m: nil, want: "error"},
		{name: "destroyed", m: &metabucket.Metadata{Status: "destroyed"}, want: "destroyed"},
		{name: "bootstrap only active status", m: &metabucket.Metadata{Status: "active"}, want: "installing"},
		{name: "bootstrap only installing", m: &metabucket.Metadata{Status: "installing"}, want: "installing"},
		{
			name: "legacy complete missing ARN",
			m:    &metabucket.Metadata{Status: "active", ClusterUID: "uid-1", IngressHost: "example.com"},
			want: "active",
		},
		{
			name: "infra done still installing",
			m:    &metabucket.Metadata{Status: "installing", ClusterARN: "arn:aws:eks:..."},
			want: "installing",
		},
		{
			name: "kubeconfig done still installing",
			m:    &metabucket.Metadata{Status: "installing", ClusterARN: "arn:...", ClusterUID: "uid"},
			want: "installing",
		},
		{
			name: "app done with installing status",
			m:    &metabucket.Metadata{Status: "installing", ClusterARN: "arn:...", IngressHost: "h.example"},
			want: "active",
		},
		{
			name: "explicit active with ARN",
			m:    &metabucket.Metadata{Status: "active", ClusterARN: "arn:..."},
			want: "active",
		},
		{
			name: "empty status with UID",
			m:    &metabucket.Metadata{ClusterUID: "uid"},
			want: "active",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clusterDisplayStatus(tt.m); got != tt.want {
				t.Fatalf("clusterDisplayStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}
