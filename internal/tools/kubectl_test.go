package tools

import "testing"

func TestIsValidIngressHost(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"was.example.com", true},
		{"wasctl.eastus.cloudapp.azure.com", true},
		{"20.241.138.136", false},
		{"", false},
		{"  ", false},
		{"::1", false},
	}
	for _, tc := range cases {
		if got := IsValidIngressHost(tc.in); got != tc.want {
			t.Errorf("IsValidIngressHost(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsACMEIssuableHost(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"was.example.com", true},
		{"a3585fbea382a49e8b7ed311f26ddb85-511901222.us-east-1.elb.amazonaws.com", false},
		{"dualstack.xxx.us-east-1.elb.amazonaws.com", false},
		{"wasctl.eastus.cloudapp.azure.com", true},
		{"20.241.138.136", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsACMEIssuableHost(tc.in); got != tc.want {
			t.Errorf("IsACMEIssuableHost(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}
