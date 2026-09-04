package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAzureBlobHost(t *testing.T) {
	got := AzureBlobHost("wolframwastfstat5ad81563")
	want := "wolframwastfstat5ad81563.blob.core.windows.net"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestWaitForAzureBlobDNSResolvesImmediately(t *testing.T) {
	origSys := lookupSystemHost
	origPub := lookupPublicHost
	origFlush := flushDNSCaches
	lookupSystemHost = func(ctx context.Context, host string) ([]string, error) {
		return []string{"20.60.1.2"}, nil
	}
	lookupPublicHost = func(ctx context.Context, host string) ([]string, error) {
		t.Fatal("public DNS should not be consulted when local succeeds")
		return nil, nil
	}
	flushDNSCaches = func() {}
	t.Cleanup(func() {
		lookupSystemHost = origSys
		lookupPublicHost = origPub
		flushDNSCaches = origFlush
	})

	var logs []string
	err := WaitForAzureBlobDNS(context.Background(), "acct", func(line string) { logs = append(logs, line) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) < 2 {
		t.Fatalf("expected start + success logs, got %v", logs)
	}
}

func TestWaitForAzureBlobDNSRequiresLocalResolver(t *testing.T) {
	origSys := lookupSystemHost
	origPub := lookupPublicHost
	origFlush := flushDNSCaches
	origMax := AzureBlobDNSMaxWait
	origInterval := azureBlobDNSInterval

	calls := 0
	lookupSystemHost = func(ctx context.Context, host string) ([]string, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("lookup failed: i/o timeout")
		}
		return []string{"20.60.1.2"}, nil
	}
	lookupPublicHost = func(ctx context.Context, host string) ([]string, error) {
		return []string{"20.60.1.2"}, nil
	}
	flushed := 0
	flushDNSCaches = func() { flushed++ }
	AzureBlobDNSMaxWait = 5 * time.Second
	azureBlobDNSInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		lookupSystemHost = origSys
		lookupPublicHost = origPub
		flushDNSCaches = origFlush
		AzureBlobDNSMaxWait = origMax
		azureBlobDNSInterval = origInterval
	})

	var logs []string
	err := WaitForAzureBlobDNS(context.Background(), "acct", func(line string) { logs = append(logs, line) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flushed == 0 {
		t.Fatal("expected DNS cache flush while waiting for local resolver")
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "exists in public DNS") {
		t.Fatalf("expected public-DNS diagnostic log; got %v", logs)
	}
	if !strings.Contains(joined, "resolved via local resolver") {
		t.Fatalf("expected local resolver success log; got %v", logs)
	}
}

func TestWaitForAzureBlobDNSEmptyAccount(t *testing.T) {
	err := WaitForAzureBlobDNS(context.Background(), "", func(string) {})
	if err == nil {
		t.Fatal("expected error for empty account")
	}
}

func TestWaitForAzureBlobDNSTimeoutHintsPublicSeen(t *testing.T) {
	origSys := lookupSystemHost
	origPub := lookupPublicHost
	origFlush := flushDNSCaches
	origMax := AzureBlobDNSMaxWait
	origInterval := azureBlobDNSInterval

	lookupSystemHost = func(ctx context.Context, host string) ([]string, error) {
		return nil, errors.New("lookup failed: i/o timeout")
	}
	lookupPublicHost = func(ctx context.Context, host string) ([]string, error) {
		return []string{"20.60.1.2"}, nil
	}
	flushDNSCaches = func() {}
	AzureBlobDNSMaxWait = 50 * time.Millisecond
	azureBlobDNSInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		lookupSystemHost = origSys
		lookupPublicHost = origPub
		flushDNSCaches = origFlush
		AzureBlobDNSMaxWait = origMax
		azureBlobDNSInterval = origInterval
	})

	err := WaitForAzureBlobDNS(context.Background(), "acct", func(string) {})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "exists in public DNS") {
		t.Fatalf("expected public-DNS hint in error; got %v", err)
	}
}
