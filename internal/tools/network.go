package tools

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"time"
)

const azureBlobLookupTimeout = 20 * time.Second

// azureBlobDNSInterval is the sleep between local-resolver polls.
var azureBlobDNSInterval = 15 * time.Second

// AzureBlobDNSMaxWait is how long to poll for a new storage account blob endpoint
// to become resolvable by the local system resolver (what Terraform uses).
var AzureBlobDNSMaxWait = 10 * time.Minute

// azureBlobFallbackDNSServers are used only as a diagnostic probe when the host
// resolver (systemd-resolved at 127.0.0.53) times out. Success via fallback proves
// the name exists in public DNS but is NOT enough for Terraform — WaitForAzureBlobDNS
// only returns success once the system/default resolver also succeeds.
var azureBlobFallbackDNSServers = []string{
	"8.8.8.8:53",
	"1.1.1.1:53",
}

var (
	lookupSystemHost = func(ctx context.Context, host string) ([]string, error) {
		ctx, cancel := context.WithTimeout(ctx, azureBlobLookupTimeout)
		defer cancel()
		return net.DefaultResolver.LookupHost(ctx, host)
	}
	lookupPublicHost = lookupAzureBlobViaPublicDNS
	flushDNSCaches   = flushSystemDNSCaches
)

// AzureBlobHost returns the blob service hostname for a storage account.
func AzureBlobHost(storageAccount string) string {
	return storageAccount + ".blob.core.windows.net"
}

func lookupAzureBlobViaPublicDNS(ctx context.Context, host string) ([]string, error) {
	var lastErr error
	for _, server := range azureBlobFallbackDNSServers {
		r := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: azureBlobLookupTimeout}
				return d.DialContext(ctx, "tcp", server)
			},
		}
		ctx2, cancel2 := context.WithTimeout(ctx, azureBlobLookupTimeout)
		addrs, err := r.LookupHost(ctx2, host)
		cancel2()
		if err == nil && len(addrs) > 0 {
			return addrs, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	return nil, lastErr
}

func flushSystemDNSCaches() {
	// Best-effort: systemd-resolved often holds a stale NXDOMAIN/timeout after a
	// brand-new Azure storage account is created. Ignore failures (no systemd,
	// no permissions, non-Linux, etc.).
	_ = exec.Command("resolvectl", "flush-caches").Run()
}

// WaitForAzureBlobDNS blocks until the storage account blob endpoint resolves
// via the local system resolver — the same path Terraform uses.
//
// Azure DNS can take 1–3 minutes after account creation. Local stub resolvers
// (systemd-resolved) may also time out briefly. Public DNS (8.8.8.8 / 1.1.1.1)
// is queried only as a diagnostic signal that the name exists globally; success
// still requires the system resolver to succeed so terraform init can proceed.
func WaitForAzureBlobDNS(ctx context.Context, storageAccount string, log func(string)) error {
	if storageAccount == "" {
		return fmt.Errorf("storage account name is empty")
	}
	host := AzureBlobHost(storageAccount)
	deadline := time.Now().Add(AzureBlobDNSMaxWait)
	attempt := 0
	var lastErr error
	publicSeen := false

	log(fmt.Sprintf("[dns] waiting for local resolver to resolve %s (up to %s)…", host, AzureBlobDNSMaxWait))

	for {
		attempt++
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			hint := ""
			if publicSeen {
				hint = "; name exists in public DNS but local resolver (e.g. 127.0.0.53) still fails — try: resolvectl flush-caches"
			}
			if lastErr != nil {
				return fmt.Errorf("azure blob DNS for %s not ready after %s%s: %w", host, AzureBlobDNSMaxWait, hint, lastErr)
			}
			return fmt.Errorf("azure blob DNS for %s not ready after %s%s", host, AzureBlobDNSMaxWait, hint)
		}

		addrs, err := lookupSystemHost(ctx, host)
		if err == nil && len(addrs) > 0 {
			log(fmt.Sprintf("[dns] %s resolved via local resolver (%d address(es))", host, len(addrs)))
			return nil
		}
		lastErr = err

		if pub, pubErr := lookupPublicHost(ctx, host); pubErr == nil && len(pub) > 0 {
			if !publicSeen {
				publicSeen = true
				log(fmt.Sprintf("[dns] %s exists in public DNS (%d address(es)); waiting for local resolver…", host, len(pub)))
			}
			flushDNSCaches()
		}

		log(fmt.Sprintf("[dns] local resolver not ready (attempt %d): %v", attempt, lastErr))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(azureBlobDNSInterval):
		}
	}
}
