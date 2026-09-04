package audit

import (
	"testing"
	"time"
)

func TestFormatAge(t *testing.T) {
	cases := []struct {
		diff time.Duration
		want string
	}{
		{10 * time.Second, "just now"},
		{90 * time.Second, "1m ago"},
		{2 * time.Hour, "2h ago"},
		{50 * time.Hour, "2d ago"},
	}
	for _, tc := range cases {
		e := Entry{Time: time.Now().Add(-tc.diff)}
		got := FormatAge(e)
		if got != tc.want {
			t.Errorf("FormatAge(%v): got %q, want %q", tc.diff, got, tc.want)
		}
	}
}

func TestAuditLogKey(t *testing.T) {
	got := AuditLogKey("was-prod")
	want := "clusters/was-prod/audit.log"
	if got != want {
		t.Errorf("AuditLogKey: got %q, want %q", got, want)
	}
}

func TestNoopLog(t *testing.T) {
	var n Noop
	if err := n.Log(nil, "cluster", "install", nil, "success"); err != nil {
		t.Errorf("Noop.Log: unexpected error %v", err)
	}
}
