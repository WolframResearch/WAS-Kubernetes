package metacontainer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ── in-memory mock blobDataClient ─────────────────────────────────────────────

type memClient struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func newMemClient() *memClient {
	return &memClient{blobs: make(map[string][]byte)}
}

func (m *memClient) get(_ context.Context, name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.blobs[name]
	if !ok {
		return nil, &ErrNotFound{Key: name}
	}
	cp := make([]byte, len(d))
	copy(cp, d)
	return cp, nil
}

func (m *memClient) put(_ context.Context, name string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	m.blobs[name] = cp
	return nil
}

func (m *memClient) del(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blobs, name)
	return nil
}

func (m *memClient) exists(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.blobs[name]
	return ok, nil
}

func (m *memClient) listPrefixes(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	for k := range m.blobs {
		if len(k) < len(prefix) || k[:len(prefix)] != prefix {
			continue
		}
		rest := k[len(prefix):]
		slash := -1
		for i, c := range rest {
			if c == '/' {
				slash = i
				break
			}
		}
		var virtual string
		if slash >= 0 {
			virtual = prefix + rest[:slash+1]
		} else {
			virtual = prefix + rest
		}
		seen[virtual] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out, nil
}

// newTestContainer builds a Container backed by the in-memory mock (no Azure cred).
func newTestContainer(subscriptionID string) (*Container, *memClient) {
	mem := newMemClient()
	c := &Container{
		data:           mem,
		accountName:    StorageAccountName(subscriptionID, "testcluster"),
		subscriptionID: subscriptionID,
		cred:           nil,
	}
	return c, mem
}

// ── mock blobLeaseOps ──────────────────────────────────────────────────────────

type mockLeaseOps struct {
	mu       sync.Mutex
	heldBy   string
	conflict bool // if true, acquire always returns ErrLockConflict
}

func (m *mockLeaseOps) acquire(_ context.Context, _ int32) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conflict || m.heldBy != "" {
		return "", &ErrLockConflict{}
	}
	m.heldBy = "test"
	return "test-lease-id", nil
}

func (m *mockLeaseOps) renew(_ context.Context, _ string) error { return nil }

func (m *mockLeaseOps) release(_ context.Context, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heldBy = ""
	return nil
}

func (m *mockLeaseOps) breakLease(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heldBy = ""
	return nil
}

// ── key helper tests ───────────────────────────────────────────────────────────

func TestStorageAccountName(t *testing.T) {
	sub := "12345678-abcd-efef-1234-abcdef012345"
	got := StorageAccountName(sub, "wasctl")
	want := "wolframwasctl12345678"
	if got != want {
		t.Errorf("StorageAccountName: got %q, want %q", got, want)
	}
	if len(got) > 24 {
		t.Errorf("StorageAccountName too long: %d chars", len(got))
	}
}

func TestStorageAccountNameShortSub(t *testing.T) {
	// Subscription ID shorter than 8 hex chars after stripping hyphens.
	got := StorageAccountName("12-56", "wasctl")
	if len(got) < 11 {
		t.Errorf("StorageAccountName too short: %q", got)
	}
}

func TestKeyHelpers(t *testing.T) {
	cluster := "was-prod"
	tests := []struct {
		fn   func(string) string
		want string
	}{
		{ClusterKeyPrefix, "clusters/was-prod/"},
		{BootstrapStateKey, "clusters/was-prod/bootstrap-state/terraform.tfstate"},
		{BootstrapStateBackupKey, "clusters/was-prod/bootstrap-state/terraform.tfstate.backup"},
		{BackendTFVarsKey, "clusters/was-prod/backend.tfvars"},
		{WorkspaceMetaKey, "clusters/was-prod/workspace.json"},
		{LockBlobKey, "locks/was-prod"},
	}
	for _, tt := range tests {
		if got := tt.fn(cluster); got != tt.want {
			t.Errorf("key %q: got %q, want %q", cluster, got, tt.want)
		}
	}
}

// ── container CRUD tests ───────────────────────────────────────────────────────

func TestContainerGetPutDelete(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestContainer("12345678-0000-0000-0000-000000000000")

	if err := c.Put(ctx, "test/key", []byte("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	data, err := c.Get(ctx, "test/key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("Get: got %q, want %q", data, "hello")
	}

	if err := c.Delete(ctx, "test/key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = c.Get(ctx, "test/key")
	var nf *ErrNotFound
	if !errors.As(err, &nf) {
		t.Errorf("Get after Delete: expected ErrNotFound, got %v", err)
	}
}

func TestContainerPutIdempotent(t *testing.T) {
	ctx := context.Background()
	c, mem := newTestContainer("12345678-0000-0000-0000-000000000000")

	if err := c.Put(ctx, "k", []byte("data")); err != nil {
		t.Fatal(err)
	}
	before, _ := mem.get(ctx, "k")

	// Put same content again — should skip the write (SHA-256 match).
	if err := c.Put(ctx, "k", []byte("data")); err != nil {
		t.Fatal(err)
	}
	after, _ := mem.get(ctx, "k")
	if string(before) != string(after) {
		t.Error("Put idempotency: content changed on identical Put")
	}
}

func TestContainerPutOverwrites(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestContainer("12345678-0000-0000-0000-000000000000")

	_ = c.Put(ctx, "k", []byte("v1"))
	_ = c.Put(ctx, "k", []byte("v2"))
	got, _ := c.Get(ctx, "k")
	if string(got) != "v2" {
		t.Errorf("Put overwrite: got %q, want %q", got, "v2")
	}
}

func TestContainerExists(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestContainer("12345678-0000-0000-0000-000000000000")

	ok, err := c.Exists(ctx, "absent")
	if err != nil || ok {
		t.Errorf("Exists absent: (%v, %v)", ok, err)
	}

	_ = c.Put(ctx, "present", []byte("x"))
	ok, err = c.Exists(ctx, "present")
	if err != nil || !ok {
		t.Errorf("Exists present: (%v, %v)", ok, err)
	}
}

func TestContainerDeleteIdempotent(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestContainer("12345678-0000-0000-0000-000000000000")

	if err := c.Delete(ctx, "nonexistent"); err != nil {
		t.Errorf("Delete nonexistent: %v", err)
	}
}

func TestListClusters(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestContainer("12345678-0000-0000-0000-000000000000")

	for _, name := range []string{"alpha", "beta", "gamma"} {
		_ = c.Put(ctx, WorkspaceMetaKey(name), []byte("{}"))
	}

	names, err := c.ListClusters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Errorf("ListClusters: got %d, want 3: %v", len(names), names)
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !found[want] {
			t.Errorf("ListClusters: missing %q", want)
		}
	}
}

// ── error type tests ───────────────────────────────────────────────────────────

func TestErrNotFound(t *testing.T) {
	err := &ErrNotFound{Key: "clusters/was-prod/workspace.json"}
	if err.Error() == "" {
		t.Error("ErrNotFound.Error() is empty")
	}
}

func TestErrWorkspaceNotFound(t *testing.T) {
	err := &ErrWorkspaceNotFound{ClusterName: "mycluster", SubscriptionID: "sub-123"}
	if err.Error() == "" {
		t.Error("ErrWorkspaceNotFound.Error() is empty")
	}
}

func TestErrLockConflict(t *testing.T) {
	err := &ErrLockConflict{ClusterName: "was-prod"}
	if err.Error() == "" {
		t.Error("ErrLockConflict.Error() is empty")
	}
}

// ── lock tests ─────────────────────────────────────────────────────────────────

func TestBlobLockAcquireRelease(t *testing.T) {
	ctx := context.Background()
	ops := &mockLeaseOps{}
	lock := &BlobLock{ops: ops, name: "was-prod", holder: "test"}

	if err := lock.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// After release, can acquire again.
	if err := lock.Acquire(ctx); err != nil {
		t.Fatalf("re-Acquire: %v", err)
	}
	_ = lock.Release(ctx)
}

func TestBlobLockConflict(t *testing.T) {
	ctx := context.Background()
	ops := &mockLeaseOps{conflict: true}
	lock := &BlobLock{ops: ops, name: "was-prod", holder: "h"}

	err := lock.Acquire(ctx)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	// Caller should get a descriptive message (not the raw ErrLockConflict).
	if errors.Is(err, &ErrLockConflict{}) {
		t.Error("expected wrapped message, not raw ErrLockConflict")
	}
}

func TestBlobLockReleaseIdempotent(t *testing.T) {
	ctx := context.Background()
	ops := &mockLeaseOps{}
	lock := &BlobLock{ops: ops, name: "c", holder: "h"}

	_ = lock.Acquire(ctx)
	if err := lock.Release(ctx); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := lock.Release(ctx); err != nil {
		t.Fatalf("second Release (idempotent): %v", err)
	}
}

func TestBlobLockForceRelease(t *testing.T) {
	ctx := context.Background()
	ops := &mockLeaseOps{}
	lock := &BlobLock{ops: ops, name: "c", holder: "h"}

	_ = lock.Acquire(ctx)
	if err := lock.ForceRelease(ctx); err != nil {
		t.Fatalf("ForceRelease: %v", err)
	}
	if ops.heldBy != "" {
		t.Error("ForceRelease did not clear the lock")
	}
}

func TestEnsureLockBlob(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestContainer("12345678-0000-0000-0000-000000000000")

	if err := EnsureLockBlob(ctx, c, "was-prod"); err != nil {
		t.Fatalf("EnsureLockBlob: %v", err)
	}
	// Idempotent — second call should not error.
	if err := EnsureLockBlob(ctx, c, "was-prod"); err != nil {
		t.Fatalf("EnsureLockBlob (idempotent): %v", err)
	}
	ok, _ := c.Exists(ctx, LockBlobKey("was-prod"))
	if !ok {
		t.Error("lock blob not created")
	}
}

// ── metadata tests ─────────────────────────────────────────────────────────────

func TestMetadataRoundtrip(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestContainer("12345678-0000-0000-0000-000000000000")

	m := NewAzureMetadata("was-prod", "sub-123", "my-rg", "eastus", "state-rg", "sa123", "1.2.0")
	if err := WriteMetadata(ctx, c, m, "1.2.0"); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}

	got, err := ReadMetadata(ctx, c, "was-prod")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if got.ClusterName != "was-prod" {
		t.Errorf("ClusterName: %q", got.ClusterName)
	}
	if got.Cloud != "azure" {
		t.Errorf("Cloud: %q", got.Cloud)
	}
	if got.AzureSubscriptionID != "sub-123" {
		t.Errorf("AzureSubscriptionID: %q", got.AzureSubscriptionID)
	}
	if got.AzureResourceGroup != "my-rg" {
		t.Errorf("AzureResourceGroup: %q", got.AzureResourceGroup)
	}
	if got.Status != "installing" {
		t.Errorf("Status: %q", got.Status)
	}
}

func TestMetadataNotFound(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestContainer("12345678-0000-0000-0000-000000000000")

	_, err := ReadMetadata(ctx, c, "nonexistent")
	var nf *ErrWorkspaceNotFound
	if !errors.As(err, &nf) {
		t.Errorf("expected ErrWorkspaceNotFound, got %T: %v", err, err)
	}
}

func TestNewAzureMetadataDefaults(t *testing.T) {
	m := NewAzureMetadata("c", "s", "rg", "loc", "srg", "sa", "1.0.0")
	if m.Status != "installing" {
		t.Errorf("Status: %q", m.Status)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("SchemaVersion: %d", m.SchemaVersion)
	}
	if m.Cloud != "azure" {
		t.Errorf("Cloud: %q", m.Cloud)
	}
	if m.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if time.Since(m.CreatedAt) > 5*time.Second {
		t.Errorf("CreatedAt looks stale: %v ago", time.Since(m.CreatedAt))
	}
}

func TestWriteMetadataUpdatesModifiedAt(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestContainer("12345678-0000-0000-0000-000000000000")
	m := NewAzureMetadata("c", "s", "rg", "loc", "srg", "sa", "1.0.0")
	original := m.LastModifiedAt

	time.Sleep(2 * time.Millisecond) // ensure time advances
	if err := WriteMetadata(ctx, c, m, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if !m.LastModifiedAt.After(original) {
		t.Error("WriteMetadata did not advance LastModifiedAt")
	}
}

func TestContainerName(t *testing.T) {
	sub := "12345678-0000-0000-0000-000000000000"
	c, _ := newTestContainer(sub)
	if c.Name() == "" {
		t.Error("Container.Name() is empty")
	}
	if c.Name() != StorageAccountName(sub, "testcluster") {
		t.Errorf("Container.Name(): got %q, want %q", c.Name(), StorageAccountName(sub, "testcluster"))
	}
}
