// Package workspace manages the per-cluster cloud-backed workspace.
//
// A workspace is the full set of durable state for one cluster: the bootstrap
// terraform.tfstate, the stack backend config, and workspace.json. For AWS,
// all of it lives in the per-account meta S3 bucket. For Azure, it lives in
// the per-subscription meta storage account.
//
// Typical stage usage:
//
//	w, err := workspace.OpenOrCreateForCloud(ctx, cfg, "was-prod")
//	if err != nil { return err }
//	if err := w.Lock(ctx); err != nil { return err }
//	defer w.Close()
//
//	mat, err := w.Materialize(ctx, assets.BootstrapFS, workspace.Bootstrap)
//	if err != nil { return err }
//	defer w.Persist(ctx, mat)
//
//	// … run terraform against mat.BootstrapDir …
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
)

// metaStore is the cloud-agnostic blob store interface.
// Implemented by *metabucket.Bucket (AWS) and *metacontainer.Container (Azure).
type metaStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	ListClusters(ctx context.Context) ([]string, error)
	Name() string
}

// metaLock is the cloud-agnostic distributed lock interface.
// Implemented by *metabucket.Lock (AWS DynamoDB) and *metacontainer.BlobLock (Azure).
type metaLock interface {
	Acquire(ctx context.Context) error
	Release(ctx context.Context) error
	ForceRelease(ctx context.Context) error
}

// AssetKind identifies which set of embedded assets to materialise.
type AssetKind int

const (
	Bootstrap AssetKind = iota // bootstrap .tf files + state
	Stack                      // stack .tf files + backend config
	Chart                      // Helm chart files
)

// Workspace holds an open handle to a cluster's cloud workspace.
type Workspace struct {
	Meta    *metabucket.Metadata // workspace.json content (mutable)
	store   metaStore            // was bucket *metabucket.Bucket
	lock    metaLock             // was lock *metabucket.Lock
	tempDir string               // set by Materialize, cleared by Close
}

// Materialized holds the local temp paths populated by Materialize.
type Materialized struct {
	// Exactly one of these is non-empty depending on AssetKind.
	BootstrapDir string
	StackDir     string
	ChartDir     string
	// Set by w.Kubeconfig(); empty until called.
	KubeconfigPath string

	kind    AssetKind
	tempDir string
}

// ── AWS constructors ──────────────────────────────────────────────────────────

// Open finds a cluster workspace in the AWS meta bucket.
// Returns *metabucket.ErrWorkspaceNotFound if it doesn't exist.
func Open(ctx context.Context, metaRegion, accountID, clusterName string) (*Workspace, error) {
	b, err := metabucket.Open(ctx, metaRegion, accountID, clusterName)
	if err != nil {
		return nil, err
	}
	meta, err := readMeta(ctx, b, clusterName)
	if err != nil {
		return nil, err
	}
	lk, err := metabucket.NewLock(ctx, metaRegion, accountID, clusterName)
	if err != nil {
		return nil, err
	}
	return &Workspace{Meta: meta, store: b, lock: lk}, nil
}

// Create initialises a new AWS workspace. Errors if one already exists.
func Create(ctx context.Context, metaRegion, accountID, clusterName, region string) (*Workspace, error) {
	b, err := metabucket.EnsureExists(ctx, metaRegion, accountID, clusterName)
	if err != nil {
		return nil, err
	}
	if err := metabucket.EnsureLockTable(ctx, metaRegion, accountID, clusterName); err != nil {
		return nil, err
	}

	if _, readErr := readMeta(ctx, b, clusterName); readErr == nil {
		return nil, fmt.Errorf("workspace for cluster %q already exists; use OpenOrCreate", clusterName)
	} else {
		var nf *metabucket.ErrWorkspaceNotFound
		if !errors.As(readErr, &nf) {
			return nil, readErr
		}
	}

	meta := metabucket.NewMetadata(clusterName, accountID, region, version.Version)
	if err := writeMeta(ctx, b, meta, version.Version); err != nil {
		return nil, err
	}
	lk, err := metabucket.NewLock(ctx, metaRegion, accountID, clusterName)
	if err != nil {
		return nil, err
	}
	return &Workspace{Meta: meta, store: b, lock: lk}, nil
}

// OpenOrCreate is the common case for AWS: opens an existing workspace or creates one.
func OpenOrCreate(ctx context.Context, metaRegion, accountID, clusterName, region string) (*Workspace, error) {
	b, err := metabucket.EnsureExists(ctx, metaRegion, accountID, clusterName)
	if err != nil {
		return nil, err
	}
	if err := metabucket.EnsureLockTable(ctx, metaRegion, accountID, clusterName); err != nil {
		return nil, err
	}

	meta, readErr := readMeta(ctx, b, clusterName)
	if readErr != nil {
		var nf *metabucket.ErrWorkspaceNotFound
		if !errors.As(readErr, &nf) {
			return nil, readErr
		}
		meta = metabucket.NewMetadata(clusterName, accountID, region, version.Version)
		if err := writeMeta(ctx, b, meta, version.Version); err != nil {
			return nil, err
		}
	}

	lk, err := metabucket.NewLock(ctx, metaRegion, accountID, clusterName)
	if err != nil {
		return nil, err
	}
	return &Workspace{Meta: meta, store: b, lock: lk}, nil
}

// ── Azure constructors ────────────────────────────────────────────────────────

// OpenAzure finds a cluster workspace in the Azure meta container.
// Returns *metacontainer.ErrWorkspaceNotFound if it doesn't exist.
// Ensures the lock blob exists (for stages that will Acquire).
func OpenAzure(ctx context.Context, subscriptionID, clusterName string) (*Workspace, error) {
	return openAzure(ctx, subscriptionID, clusterName, true)
}

// OpenAzureReadOnly opens an Azure workspace for UI/inspect without creating
// or touching the lock blob. Prefer this for read-only web UI paths.
func OpenAzureReadOnly(ctx context.Context, subscriptionID, clusterName string) (*Workspace, error) {
	return openAzure(ctx, subscriptionID, clusterName, false)
}

func openAzure(ctx context.Context, subscriptionID, clusterName string, ensureLock bool) (*Workspace, error) {
	c, err := metacontainer.Open(ctx, subscriptionID, clusterName)
	if err != nil {
		return nil, err
	}
	meta, err := metacontainer.ReadMetadata(ctx, c, clusterName)
	if err != nil {
		return nil, err
	}
	if ensureLock {
		if err := metacontainer.EnsureLockBlob(ctx, c, clusterName); err != nil {
			return nil, err
		}
	}
	var lk metaLock
	if ensureLock {
		var lerr error
		lk, lerr = metacontainer.NewLock(c, clusterName)
		if lerr != nil {
			return nil, lerr
		}
	}
	return &Workspace{Meta: meta, store: c, lock: lk}, nil
}

// OpenOrCreateAzure opens an existing Azure workspace or creates one.
// metaResourceGroup is the wasctl meta storage RG (wolfram-<cluster>-meta).
// AzureResourceGroup in workspace.json is the AKS stack RG (<cluster>-rg).
func OpenOrCreateAzure(ctx context.Context, subscriptionID, metaResourceGroup, location, clusterName string) (*Workspace, error) {
	c, err := metacontainer.EnsureExists(ctx, subscriptionID, metaResourceGroup, location, clusterName)
	if err != nil {
		return nil, err
	}

	meta, readErr := metacontainer.ReadMetadata(ctx, c, clusterName)
	if readErr != nil {
		var nf *metacontainer.ErrWorkspaceNotFound
		if !errors.As(readErr, &nf) {
			return nil, readErr
		}
		// Stack RG for AKS (not the meta RG passed to EnsureExists).
		meta = metacontainer.NewAzureMetadata(
			clusterName, subscriptionID, AzureStackResourceGroup(clusterName), location, "", "", version.Version)
		if err := metacontainer.WriteMetadata(ctx, c, meta, version.Version); err != nil {
			return nil, err
		}
	}

	if err := metacontainer.EnsureLockBlob(ctx, c, clusterName); err != nil {
		return nil, err
	}
	lk, err := metacontainer.NewLock(c, clusterName)
	if err != nil {
		return nil, err
	}
	return &Workspace{Meta: meta, store: c, lock: lk}, nil
}

// ── Cloud-routing constructors (for stages) ───────────────────────────────────

// OpenForCloud opens an existing workspace, routing to AWS or Azure based on cfg.Cloud.
// For AWS: delegates to Open using cfg.MetaRegion and the AWS account ID.
// For Azure: delegates to OpenAzure using the subscription ID from cfg.
//
// The caller must supply accountID (from GetCallerIdentity for AWS) or
// subscriptionID (from GetAccountInfo for Azure).
func OpenForCloud(ctx context.Context, cfg *config.Config, cloudAccountID, clusterName string) (*Workspace, error) {
	switch cfg.Cloud {
	case "azure":
		return OpenAzure(ctx, cloudAccountID, clusterName)
	default:
		return Open(ctx, cfg.MetaRegion.Value, cloudAccountID, clusterName)
	}
}

// OpenOrCreateForCloud opens or creates a workspace, routing on cfg.Cloud.
// extraArgs carries cloud-specific parameters:
//   - AWS: [region]  (e.g. "us-east-1")
//   - Azure: [resourceGroup, location]
func OpenOrCreateForCloud(ctx context.Context, cfg *config.Config, cloudAccountID, clusterName string, extraArgs ...string) (*Workspace, error) {
	switch cfg.Cloud {
	case "azure":
		rg, loc := "", ""
		if len(extraArgs) >= 1 {
			rg = extraArgs[0]
		}
		if len(extraArgs) >= 2 {
			loc = extraArgs[1]
		}
		return OpenOrCreateAzure(ctx, cloudAccountID, rg, loc, clusterName)
	default:
		region := cfg.Region.Value
		if len(extraArgs) >= 1 {
			region = extraArgs[0]
		}
		return OpenOrCreate(ctx, cfg.MetaRegion.Value, cloudAccountID, clusterName, region)
	}
}

// ── Workspace methods ─────────────────────────────────────────────────────────

// CheckCloudMatch returns an error when cfg.Cloud disagrees with the cloud
// recorded in this workspace's metadata. Call after Open on any stage that
// modifies cluster state.
//
// An empty Meta.Cloud is treated as "aws" for workspaces created before Azure support.
func (w *Workspace) CheckCloudMatch(cfgCloud string) error {
	want := cfgCloud
	if want == "" {
		want = "aws"
	}
	got := w.Meta.Cloud
	if got == "" {
		got = "aws"
	}
	if got == want {
		return nil
	}
	return fmt.Errorf(
		"workspace %q was created for cloud %q but --cloud %q was passed.\n"+
			"To fix: run with --cloud %s, or destroy and recreate the cluster for the other cloud.",
		w.Meta.ClusterName, got, want, got,
	)
}

// Lock acquires the per-cluster lock. Must be called before
// Materialize/Persist on any state-mutating operation.
func (w *Workspace) Lock(ctx context.Context) error {
	return w.lock.Acquire(ctx)
}

// Unlock releases the per-cluster lock. Idempotent.
func (w *Workspace) Unlock(ctx context.Context) {
	_ = w.lock.Release(ctx)
}

// ForceUnlock releases the lock unconditionally (wasctl unlock command).
func (w *Workspace) ForceUnlock(ctx context.Context) error {
	return w.lock.ForceRelease(ctx)
}

// Materialize downloads workspace state and writes asset files to a temp directory.
func (w *Workspace) Materialize(ctx context.Context, assetsFS fs.FS, kind AssetKind) (Materialized, error) {
	if w.tempDir != "" {
		_ = os.RemoveAll(w.tempDir)
		w.tempDir = ""
	}

	dir, err := os.MkdirTemp("", fmt.Sprintf("wasctl-%s-*", w.Meta.ClusterName))
	if err != nil {
		return Materialized{}, fmt.Errorf("create temp dir: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		_ = os.RemoveAll(dir)
		return Materialized{}, fmt.Errorf("chmod temp dir: %w", err)
	}
	w.tempDir = dir

	mat := Materialized{kind: kind, tempDir: dir}

	switch kind {
	case Bootstrap:
		subdir := filepath.Join(dir, "bootstrap")
		if err := os.MkdirAll(subdir, 0700); err != nil {
			return Materialized{}, fmt.Errorf("mkdir bootstrap: %w", err)
		}
		if err := copyFS(assetsFS, ".", subdir); err != nil {
			return Materialized{}, fmt.Errorf("write bootstrap assets: %w", err)
		}
		if err := w.downloadIfExists(ctx, metabucket.BootstrapStateKey(w.Meta.ClusterName),
			filepath.Join(subdir, "terraform.tfstate")); err != nil {
			return Materialized{}, err
		}
		if err := w.downloadIfExists(ctx, metabucket.BootstrapStateBackupKey(w.Meta.ClusterName),
			filepath.Join(subdir, "terraform.tfstate.backup")); err != nil {
			return Materialized{}, err
		}
		mat.BootstrapDir = subdir

	case Stack:
		subdir := filepath.Join(dir, "stack")
		if err := os.MkdirAll(subdir, 0700); err != nil {
			return Materialized{}, fmt.Errorf("mkdir stack: %w", err)
		}
		if err := copyFS(assetsFS, ".", subdir); err != nil {
			return Materialized{}, fmt.Errorf("write stack assets: %w", err)
		}
		// Backend config file differs by cloud: .hcl (AWS azurerm) vs .tfvars (Azure azurerm).
		backendKey, backendFile := w.backendConfigKeyAndFile()
		if err := w.downloadIfExists(ctx, backendKey,
			filepath.Join(subdir, backendFile)); err != nil {
			return Materialized{}, err
		}
		mat.StackDir = subdir

	case Chart:
		subdir := filepath.Join(dir, "chart")
		if err := os.MkdirAll(subdir, 0700); err != nil {
			return Materialized{}, fmt.Errorf("mkdir chart: %w", err)
		}
		if err := copyFS(assetsFS, ".", subdir); err != nil {
			return Materialized{}, fmt.Errorf("write chart assets: %w", err)
		}
		mat.ChartDir = subdir
	}

	return mat, nil
}

// Persist uploads changed state files back to the meta store and writes workspace.json.
func (w *Workspace) Persist(ctx context.Context, mat Materialized) error {
	switch mat.kind {
	case Bootstrap:
		dir := mat.BootstrapDir
		if err := w.uploadIfChanged(ctx, filepath.Join(dir, "terraform.tfstate"),
			metabucket.BootstrapStateKey(w.Meta.ClusterName)); err != nil {
			return err
		}
		if err := w.uploadIfChanged(ctx, filepath.Join(dir, "terraform.tfstate.backup"),
			metabucket.BootstrapStateBackupKey(w.Meta.ClusterName)); err != nil {
			return err
		}
	case Stack:
		backendKey, backendFile := w.backendConfigKeyAndFile()
		if err := w.uploadIfChanged(ctx, filepath.Join(mat.StackDir, backendFile), backendKey); err != nil {
			return err
		}
	case Chart:
		// Nothing to persist from chart materialisation.
	}
	return writeMeta(ctx, w.store, w.Meta, version.Version)
}

// PersistMeta writes workspace.json to the meta store without uploading any
// local files. Use this when a stage only modifies w.Meta fields.
func (w *Workspace) PersistMeta(ctx context.Context) error {
	return writeMeta(ctx, w.store, w.Meta, version.Version)
}

// Close releases the temp directory. Call with defer after Materialize.
func (w *Workspace) Close() {
	if w.tempDir != "" {
		_ = os.RemoveAll(w.tempDir)
		w.tempDir = ""
	}
}

// DetachTempDir transfers ownership of the materialised temp directory to the
// caller so Close() will not delete it. Used by the web UI session cache to
// reuse an az/aws kubeconfig across tab requests.
func (w *Workspace) DetachTempDir() string {
	d := w.tempDir
	w.tempDir = ""
	return d
}

// MaterializeTempDir creates a temp dir without copying any assets.
// Used by the kubeconfig stage which needs w.tempDir set but has no assets.
func (w *Workspace) MaterializeTempDir() error {
	if w.tempDir != "" {
		_ = os.RemoveAll(w.tempDir)
		w.tempDir = ""
	}
	dir, err := os.MkdirTemp("", fmt.Sprintf("wasctl-%s-*", w.Meta.ClusterName))
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		_ = os.RemoveAll(dir)
		return fmt.Errorf("chmod temp dir: %w", err)
	}
	w.tempDir = dir
	return nil
}

// ClusterARN returns the AWS EKS cluster ARN constructed from account/region/name.
func ClusterARN(region, accountID, clusterName string) string {
	return fmt.Sprintf("arn:aws:eks:%s:%s:cluster/%s", region, accountID, clusterName)
}

// AzureStackResourceGroup returns the stack resource group that holds the AKS
// control-plane ARM resource. Matches infra/azure/stack: "${cluster_name}-rg".
// This is NOT the wasctl meta RG (wolfram-<cluster>-meta) and NOT the AKS node
// RG (MC_<rg>_<aks>_<region>).
func AzureStackResourceGroup(clusterName string) string {
	return clusterName + "-rg"
}

// AzureAKSName returns the AKS managed cluster name. Matches infra/azure/stack:
// "${cluster_name}-aks".
func AzureAKSName(clusterName string) string {
	return clusterName + "-aks"
}

// AzureNodeResourceGroup returns the default AKS-managed node resource group
// name: MC_<stackRG>_<aksName>_<location>. Azure creates this automatically;
// wasctl may need to delete orphans after a partial destroy.
func AzureNodeResourceGroup(clusterName, location string) string {
	return fmt.Sprintf("MC_%s_%s_%s", AzureStackResourceGroup(clusterName), AzureAKSName(clusterName), location)
}

// ClusterResourceID returns the AKS cluster resource ID for Azure.
// resourceGroup must be the stack RG (…-rg); clusterName must be the AKS name (…-aks).
func ClusterResourceID(subscriptionID, resourceGroup, clusterName string) string {
	return fmt.Sprintf(
		"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s",
		subscriptionID, resourceGroup, clusterName,
	)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// backendConfigKeyAndFile returns the blob/object key and the local filename
// for the Terraform backend config file. Differs by cloud:
//   - AWS: backend.hcl (passed to -backend-config for the S3 backend)
//   - Azure: backend.tfvars (passed to -backend-config for the azurerm backend)
func (w *Workspace) backendConfigKeyAndFile() (key, file string) {
	if w.Meta.Cloud == "azure" {
		return metacontainer.BackendTFVarsKey(w.Meta.ClusterName), "backend.tfvars"
	}
	return metabucket.BackendHCLKey(w.Meta.ClusterName), "backend.hcl"
}

func (w *Workspace) downloadIfExists(ctx context.Context, key, dst string) error {
	data, err := w.store.Get(ctx, key)
	if err != nil {
		if isStoreNotFound(err) {
			return nil // first run, no state yet
		}
		return fmt.Errorf("download %s: %w", key, err)
	}
	if err := os.WriteFile(dst, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

func (w *Workspace) uploadIfChanged(ctx context.Context, src, key string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", src, err)
	}
	return w.store.Put(ctx, key, data)
}

// isStoreNotFound returns true for not-found errors from either cloud store.
func isStoreNotFound(err error) bool {
	var mb *metabucket.ErrNotFound
	var mc *metacontainer.ErrNotFound
	return errors.As(err, &mb) || errors.As(err, &mc)
}

// readMeta fetches and parses workspace.json from any metaStore.
func readMeta(ctx context.Context, store metaStore, clusterName string) (*metabucket.Metadata, error) {
	key := "clusters/" + clusterName + "/workspace.json"
	data, err := store.Get(ctx, key)
	if err != nil {
		if isStoreNotFound(err) {
			return nil, &metabucket.ErrWorkspaceNotFound{
				ClusterName: clusterName,
				AccountID:   store.Name(),
			}
		}
		return nil, fmt.Errorf("read workspace metadata: %w", err)
	}
	var m metabucket.Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse workspace.json: %w", err)
	}
	const currentSchema = 1
	if m.SchemaVersion > currentSchema {
		return nil, fmt.Errorf(
			"workspace %q was created by a newer version of wasctl (schema %d, this binary understands %d).\n"+
				"Please upgrade wasctl.",
			clusterName, m.SchemaVersion, currentSchema,
		)
	}
	return &m, nil
}

// writeMeta serialises m to workspace.json in the given store.
func writeMeta(ctx context.Context, store metaStore, m *metabucket.Metadata, wasctlVersion string) error {
	m.LastModifiedAt = time.Now().UTC()
	m.LastModifiedBy = "wasctl " + wasctlVersion
	m.SchemaVersion = 1
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace.json: %w", err)
	}
	key := "clusters/" + m.ClusterName + "/workspace.json"
	return store.Put(ctx, key, data)
}

func copyFS(fsys fs.FS, root, dstDir string) error {
	return fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || path == "." {
			return nil
		}
		rel := path
		if root != "." && root != "" {
			rel = path[len(root)+1:]
		}
		dst := filepath.Join(dstDir, rel)

		if d.IsDir() {
			return os.MkdirAll(dst, 0700)
		}

		f, err := fsys.Open(path)
		if err != nil {
			return fmt.Errorf("open asset %s: %w", path, err)
		}
		defer f.Close()

		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("create %s: %w", dst, err)
		}
		defer out.Close()

		if _, err := io.Copy(out, f); err != nil {
			return fmt.Errorf("copy %s: %w", dst, err)
		}
		return nil
	})
}

// Delete removes an object from the workspace's metadata store.
func (w *Workspace) Delete(ctx context.Context, key string) error {
	return w.store.Delete(ctx, key)
}


// Note: ErrWorkspaceNotFound type alias lives in safety.go to keep the error
// surface in one place alongside VerifyCluster which also uses it.
