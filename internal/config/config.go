// Package config loads and validates wasctl configuration.
//
// Precedence (highest to lowest):
//  1. CLI flags  (source: "flag:--<name>")
//  2. Environment variables  (source: "env:<VAR>")
//  3. Config file  (source: "file:<path>")
//  4. Compiled-in defaults  (source: "default")
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/versions"
)

// Field holds a typed configuration value together with the source that set it.
// Value and Source are updated together so callers always know how a field was set.
type Field[T any] struct {
	Value  T
	Source string // e.g. "default", "env:WAS_REGION", "flag:--region", "file:wasctl.conf"
}

// String returns the string form of the field value (used for display).
func (f Field[T]) String() string { return fmt.Sprintf("%v", f.Value) }

// Config holds all resolved configuration values used by wasctl stages.
type Config struct {
	// Core deployment parameters
	Region      Field[string]
	ClusterName Field[string]
	K8sVersion  Field[string]
	IngressHost Field[string]

	// Node group sizing
	NodeType    Field[string]
	NodeMin     Field[string]
	NodeDesired Field[string]
	NodeMax     Field[string]

	// Terraform state backend (derived at bootstrap if empty)
	StateBucket Field[string]
	LockTable   Field[string]

	// Add-ons control
	AddonsSkip Field[string]

	// Kafka configuration (builtin vs external)
	KafkaMode             Field[string]
	KafkaBootstrapServers Field[string]

	// Meta bucket region — defaults to us-east-1 regardless of cluster region.
	// Override with --meta-region or WAS_META_REGION for data-residency needs.
	MetaRegion Field[string]

	// Behaviour flags (not config-file settable; flag/env only)
	Yes                 bool
	DryRun              bool
	DestroyStateBackend bool
	NoTUI               bool
	Local               bool   // read assets from local filesystem (--local dev flag)
	ChartOnly           bool   // skip infrastructure stages; manage Helm chart only
	ConfigFile          string // path to config file; default "./wasctl.conf"

	// Repo root: absolute path discovered at startup (used by --local asset reads)
	RepoRoot string

	// Cloud provider: "aws" (default) or "azure". Set via --cloud flag or WAS_CLOUD env var.
	Cloud string

	// Azure-specific: location (e.g. "eastus") and tenant ID for Terraform provider.
	AzureLocation Field[string]
	AzureTenantID Field[string]
}

// defaults returns a Config pre-populated with compiled-in default values.
func defaults() *Config {
	src := "default"
	return &Config{
		MetaRegion:  Field[string]{"us-east-1", src},
		Region:      Field[string]{"us-east-1", src},
		ClusterName: Field[string]{"was-prod", src},
		K8sVersion:  Field[string]{versions.DefaultClusterK8s("aws"), src},
		IngressHost: Field[string]{"", "unset"},
		NodeType:    Field[string]{"c5.2xlarge", src},
		NodeMin:     Field[string]{"2", src},
		NodeDesired: Field[string]{"2", src},
		NodeMax:     Field[string]{"10", src},
		StateBucket: Field[string]{"", "derived"},
		LockTable:   Field[string]{"", "derived"},
		AddonsSkip:            Field[string]{"", src},
		KafkaMode:             Field[string]{"builtin", src},
		KafkaBootstrapServers: Field[string]{"", src},
		ConfigFile:            "wasctl.conf",
		Cloud:                 "aws",
		AzureLocation:         Field[string]{"eastus", src},
		AzureTenantID:         Field[string]{"", "unset"},
	}
}

// Load builds a Config using the precedence chain. repoRoot is the absolute
// path to the wasctl repository root. overrides is a map of flag-name →
// value populated by the CLI parser before Load is called.
func Load(repoRoot, configFile string, overrides map[string]string) (*Config, error) {
	cfg := defaults()
	cfg.RepoRoot = repoRoot
	if configFile != "" {
		cfg.ConfigFile = configFile
	}

	// Layer 3: config file
	if err := cfg.applyFile(cfg.ConfigFile); err != nil {
		return nil, fmt.Errorf("config file: %w", err)
	}

	// Layer 2: environment variables
	cfg.applyEnv()

	// Layer 1: CLI flag overrides
	cfg.applyOverrides(overrides)

	// If Kubernetes version was never overridden, use the cloud-specific
	// cluster_version default from infra (AWS vs Azure may diverge).
	cfg.applyCloudK8sDefault()

	return cfg, nil
}

// applyCloudK8sDefault sets K8sVersion from infra cluster_version defaults when
// the field still carries the compiled-in "default" source.
func (c *Config) applyCloudK8sDefault() {
	if c.K8sVersion.Source != "default" {
		return
	}
	c.K8sVersion = Field[string]{versions.DefaultClusterK8s(c.Cloud), "default"}
}

// applyFile sources the config file (if it exists) as KEY=VALUE assignments.
// Lines starting with '#' and blank lines are ignored.
func (c *Config) applyFile(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil // config file is optional
	}
	if err != nil {
		return err
	}
	defer f.Close()

	src := "file:" + path
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		c.applyKV(key, val, src)
	}
	return scanner.Err()
}

// applyEnv reads exported environment variables that override defaults/file.
func (c *Config) applyEnv() {
	envMap := map[string]string{
		"WAS_META_REGION":  "",
		"WAS_REGION":       "",
		"WAS_CLUSTER_NAME": "",
		"WAS_K8S_VERSION":  "",
		"WAS_INGRESS_HOST": "",
		"WAS_NODE_TYPE":    "",
		"WAS_NODE_MIN":     "",
		"WAS_NODE_DESIRED": "",
		"WAS_NODE_MAX":     "",
		"WAS_STATE_BUCKET": "",
		"WAS_LOCK_TABLE":   "",
		"WAS_ADDONS_SKIP":             "",
		"WAS_CLOUD":                   "",
		"WAS_AZURE_LOCATION":          "",
		"WAS_AZURE_TENANT_ID":         "",
		"WAS_KAFKA_MODE":              "",
		"WAS_KAFKA_BOOTSTRAP_SERVERS": "",
	}
	for k := range envMap {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			c.applyKV(k, v, "env:"+k)
		}
	}
}

// applyOverrides applies CLI flag values (highest priority).
// Keys match flag names: "region", "cluster-name", etc.
func (c *Config) applyOverrides(overrides map[string]string) {
	for k, v := range overrides {
		if v == "" {
			continue
		}
		c.applyKV(k, v, "flag:--"+k)
	}
}

// applyKV sets one config value identified by its environment variable name
// or flag name, recording the provided source string.
func (c *Config) applyKV(key, value, source string) {
	switch strings.ToUpper(strings.ReplaceAll(key, "-", "_")) {
	case "WAS_META_REGION", "META_REGION", "META-REGION":
		c.MetaRegion = Field[string]{value, source}
	case "WAS_REGION", "REGION":
		c.Region = Field[string]{value, source}
	case "WAS_CLUSTER_NAME", "CLUSTER_NAME", "CLUSTER-NAME":
		c.ClusterName = Field[string]{value, source}
	case "WAS_K8S_VERSION", "K8S_VERSION", "K8S-VERSION":
		c.K8sVersion = Field[string]{value, source}
	case "WAS_INGRESS_HOST", "INGRESS_HOST", "INGRESS-HOST":
		c.IngressHost = Field[string]{value, source}
	case "WAS_NODE_TYPE", "NODE_TYPE", "NODE-TYPE":
		c.NodeType = Field[string]{value, source}
	case "WAS_NODE_MIN", "NODE_MIN", "NODE-MIN":
		c.NodeMin = Field[string]{value, source}
	case "WAS_NODE_DESIRED", "NODE_DESIRED", "NODE-DESIRED":
		c.NodeDesired = Field[string]{value, source}
	case "WAS_NODE_MAX", "NODE_MAX", "NODE-MAX":
		c.NodeMax = Field[string]{value, source}
	case "WAS_STATE_BUCKET", "STATE_BUCKET", "STATE-BUCKET":
		c.StateBucket = Field[string]{value, source}
	case "WAS_LOCK_TABLE", "LOCK_TABLE", "LOCK-TABLE":
		c.LockTable = Field[string]{value, source}
	case "WAS_ADDONS_SKIP", "ADDONS_SKIP", "ADDONS-SKIP", "SKIP":
		c.AddonsSkip = Field[string]{value, source}
	case "WAS_CLOUD", "CLOUD":
		c.Cloud = value
	case "WAS_AZURE_LOCATION", "AZURE_LOCATION", "LOCATION":
		c.AzureLocation = Field[string]{value, source}
	case "WAS_AZURE_TENANT_ID", "AZURE_TENANT_ID", "TENANT_ID", "TENANT-ID":
		c.AzureTenantID = Field[string]{value, source}
	case "WAS_KAFKA_MODE", "KAFKA_MODE", "KAFKA-MODE":
		c.KafkaMode = Field[string]{value, source}
	case "WAS_KAFKA_BOOTSTRAP_SERVERS", "KAFKA_BOOTSTRAP_SERVERS", "KAFKA-BOOTSTRAP-SERVERS", "KAFKA-BOOTSTRAP":
		c.KafkaBootstrapServers = Field[string]{value, source}
	}
}

// Show prints the resolved configuration table (label, value, source).
func (c *Config) Show() {
	fmt.Println()
	fmt.Println("Configuration:")
	row := func(label, value, source string) {
		fmt.Printf("  %-36s %s  (%s)\n", label+":", value, source)
	}
	row("Meta bucket region", c.MetaRegion.Value, c.MetaRegion.Source)
	row("Region", c.Region.Value, c.Region.Source)
	row("Cluster name", c.ClusterName.Value, c.ClusterName.Source)
	row("Ingress host", or(c.IngressHost.Value, "<not set>"), c.IngressHost.Source)
	row("Kubernetes version", c.K8sVersion.Value, c.K8sVersion.Source)
	row("Node instance type", c.NodeType.Value, c.NodeType.Source)
	fmt.Printf("  %-36s %s/%s/%s  (%s)\n",
		"Node count (min/desired/max):",
		c.NodeMin.Value, c.NodeDesired.Value, c.NodeMax.Value,
		c.NodeMin.Source)
	row("State bucket", or(c.StateBucket.Value, "<derived at runtime>"), c.StateBucket.Source)
	row("Lock table", or(c.LockTable.Value, "<derived at runtime>"), c.LockTable.Source)
	row("Cloud provider", c.Cloud, "flag:--cloud / env:WAS_CLOUD / interactive")
	if c.Cloud == "azure" {
		row("Azure location", c.AzureLocation.Value, c.AzureLocation.Source)
		if c.AzureTenantID.Value != "" {
			row("Azure tenant ID", c.AzureTenantID.Value, c.AzureTenantID.Source)
		}
	}
	if c.AddonsSkip.Value != "" {
		row("Skip addons", c.AddonsSkip.Value, c.AddonsSkip.Source)
	}
	if c.KafkaMode.Value != "" && c.KafkaMode.Value != "builtin" {
		row("Kafka mode", c.KafkaMode.Value, c.KafkaMode.Source)
	}
	if c.KafkaBootstrapServers.Value != "" {
		row("Kafka bootstrap", c.KafkaBootstrapServers.Value, c.KafkaBootstrapServers.Source)
	}
	fmt.Println()
}

// ValidateCloud returns an error if Cloud is not a supported value.
func (c *Config) ValidateCloud() error {
	switch c.Cloud {
	case "aws", "azure":
		return nil
	default:
		return fmt.Errorf("invalid --cloud value %q: must be \"aws\" or \"azure\"", c.Cloud)
	}
}

// StateBucketName returns the resolved state bucket name. It panics if called
// before DeriveBucketNames has been called and no explicit value was set.
func (c *Config) StateBucketName() string { return c.StateBucket.Value }

// LockTableName returns the resolved DynamoDB lock table name.
func (c *Config) LockTableName() string { return c.LockTable.Value }

// DeriveBucketNames fills in StateBucket and LockTable from the cluster name
// and the provided AWS account ID, only if they are not already set explicitly.
func (c *Config) DeriveBucketNames(accountID string) {
	if c.StateBucket.Value == "" {
		name := "wolfram-was-tfstate-" + c.ClusterName.Value + "-" + accountID
		c.StateBucket = Field[string]{name, "derived"}
	}
	if c.LockTable.Value == "" {
		name := "wolfram-was-tfstate-lock-" + c.ClusterName.Value
		c.LockTable = Field[string]{name, "derived"}
	}
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
