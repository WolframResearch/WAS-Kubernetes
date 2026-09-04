package metacontainer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/lease"
)

// ErrLockConflict is returned by BlobLock.Acquire when the blob lease is
// already held by another process. Callers check it with errors.As.
type ErrLockConflict struct {
	ClusterName string
}

func (e *ErrLockConflict) Error() string {
	return fmt.Sprintf("cluster %q is locked by another wasctl process", e.ClusterName)
}

// blobLeaseOps abstracts Azure Blob Lease operations for testability.
// The production implementation is azBlobLeaseOps; tests use mockLeaseOps.
// acquire converts Azure lease-conflict errors into *ErrLockConflict so
// BlobLock.Acquire does not need to import azcore for error detection.
type blobLeaseOps interface {
	acquire(ctx context.Context, seconds int32) (leaseID string, err error)
	renew(ctx context.Context, leaseID string) error
	release(ctx context.Context, leaseID string) error
	breakLease(ctx context.Context) error
}

// BlobLock manages a per-cluster advisory lock using Azure Blob Lease.
//
// Azure blob leases provide exclusive, server-enforced mutual exclusion:
//   - AcquireLease returns a leaseID required for renew and release.
//   - Lease duration is 60 seconds; a background goroutine renews every 45 s.
//   - If the process dies, the lease expires automatically after ≤60 s — no
//     equivalent to DynamoDB's stale-lock problem.
//
// Lock blob location: LockBlobKey(clusterName) within the meta container.
// The blob must be created before Acquire via EnsureLockBlob.
type BlobLock struct {
	ops    blobLeaseOps
	name   string // cluster name, for error messages
	holder string // "<hostname>-<pid>-<unix-ts>"

	mu      sync.Mutex
	leaseID string
	cancel  context.CancelFunc
}

// EnsureLockBlob creates the sentinel lock blob for clusterName if absent.
// Idempotent. Must be called before NewLock (or any Acquire).
func EnsureLockBlob(ctx context.Context, c *Container, clusterName string) error {
	key := LockBlobKey(clusterName)
	ok, err := c.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("check lock blob %s: %w", key, err)
	}
	if ok {
		return nil
	}
	return c.data.put(ctx, key, []byte("{}"))
}

// NewLock returns a production BlobLock backed by Azure Blob Lease.
// Call EnsureLockBlob first to create the sentinel blob.
func NewLock(c *Container, clusterName string) (*BlobLock, error) {
	if c.sharedKeyCred == nil {
		return nil, fmt.Errorf("container has no shared key credential (test containers are not lockable)")
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	holder := fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), time.Now().Unix())
	blobURL := fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s",
		c.accountName, ContainerName, LockBlobKey(clusterName))
	ops := &azBlobLeaseOps{blobURL: blobURL, sharedKeyCred: c.sharedKeyCred}
	return &BlobLock{ops: ops, name: clusterName, holder: holder}, nil
}

// Acquire acquires a 60-second renewable blob lease and starts a background
// renewal goroutine (renews every 45 s, before the 60 s TTL expires).
// Returns *ErrLockConflict if the lease is already held by another process.
func (l *BlobLock) Acquire(ctx context.Context) error {
	leaseID, err := l.ops.acquire(ctx, 60)
	if err != nil {
		var conflict *ErrLockConflict
		if errors.As(err, &conflict) {
			return fmt.Errorf(
				"another wasctl process is operating on cluster %q.\n"+
					"Wait for it to finish, or if it died, run: wasctl unlock %s",
				l.name, l.name,
			)
		}
		return fmt.Errorf("acquire lock: %w", err)
	}

	renewCtx, cancel := context.WithCancel(context.Background())
	l.mu.Lock()
	l.leaseID = leaseID
	l.cancel = cancel
	l.mu.Unlock()

	go func(id string) {
		ticker := time.NewTicker(45 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				_ = l.ops.renew(renewCtx, id)
			}
		}
	}(leaseID)

	return nil
}

// Release releases the blob lease and stops the renewal goroutine.
// Idempotent: returns nil if the lease is already gone.
func (l *BlobLock) Release(ctx context.Context) error {
	l.mu.Lock()
	cancel := l.cancel
	leaseID := l.leaseID
	l.cancel = nil
	l.leaseID = ""
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if leaseID == "" {
		return nil
	}
	return l.ops.release(ctx, leaseID)
}

// ForceRelease breaks the lease unconditionally. Used by wasctl unlock.
func (l *BlobLock) ForceRelease(ctx context.Context) error {
	l.mu.Lock()
	cancel := l.cancel
	l.cancel = nil
	l.leaseID = ""
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return l.ops.breakLease(ctx)
}

// ── Real Azure implementation ──────────────────────────────────────────────────

type azBlobLeaseOps struct {
	blobURL       string
	sharedKeyCred *azblob.SharedKeyCredential
}

func (a *azBlobLeaseOps) blobClient() (*blob.Client, error) {
	return blob.NewClientWithSharedKeyCredential(a.blobURL, a.sharedKeyCred, nil)
}

func (a *azBlobLeaseOps) acquire(ctx context.Context, seconds int32) (string, error) {
	blobCli, err := a.blobClient()
	if err != nil {
		return "", fmt.Errorf("blob client: %w", err)
	}
	leaseCli, err := lease.NewBlobClient(blobCli, nil)
	if err != nil {
		return "", fmt.Errorf("lease client: %w", err)
	}
	resp, err := leaseCli.AcquireLease(ctx, int32(seconds), nil)
	if err != nil {
		if isAzureLeaseConflict(err) {
			return "", &ErrLockConflict{}
		}
		return "", fmt.Errorf("acquire blob lease: %w", err)
	}
	if resp.LeaseID == nil {
		return "", fmt.Errorf("acquire lease: nil LeaseID in response")
	}
	return *resp.LeaseID, nil
}

func (a *azBlobLeaseOps) renew(ctx context.Context, leaseID string) error {
	blobCli, err := a.blobClient()
	if err != nil {
		return err
	}
	leaseCli, err := lease.NewBlobClient(blobCli, &lease.BlobClientOptions{LeaseID: &leaseID})
	if err != nil {
		return err
	}
	_, err = leaseCli.RenewLease(ctx, nil)
	return wrapErr(err, "renew lease")
}

func (a *azBlobLeaseOps) release(ctx context.Context, leaseID string) error {
	blobCli, err := a.blobClient()
	if err != nil {
		return err
	}
	leaseCli, err := lease.NewBlobClient(blobCli, &lease.BlobClientOptions{LeaseID: &leaseID})
	if err != nil {
		return err
	}
	_, err = leaseCli.ReleaseLease(ctx, nil)
	if err != nil && isLeaseGone(err) {
		return nil
	}
	return wrapErr(err, "release lease")
}

func (a *azBlobLeaseOps) breakLease(ctx context.Context) error {
	blobCli, err := a.blobClient()
	if err != nil {
		return err
	}
	leaseCli, err := lease.NewBlobClient(blobCli, nil)
	if err != nil {
		return err
	}
	_, err = leaseCli.BreakLease(ctx, nil)
	return wrapErr(err, "break lease")
}

// isLeaseGone returns true for 404 (blob deleted) and 412 (lease expired/lost) —
// both mean the lease is already gone; Release should be a no-op.
func isLeaseGone(err error) bool {
	var re *azcore.ResponseError
	if !errors.As(err, &re) {
		return false
	}
	return re.StatusCode == http.StatusNotFound || re.StatusCode == http.StatusPreconditionFailed
}
