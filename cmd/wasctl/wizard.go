package main

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/versions"
)

// Interactive install wizard — mirrors the web UI steps (basics → networking →
// capacity → optional add-ons), then the caller shows a final confirm.

var (
	cliClusterNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9\-]{0,98}[a-zA-Z0-9]$|^[a-zA-Z]$`)

	cliEKSRegions = []string{
		"us-east-1", "us-east-2", "us-west-1", "us-west-2",
		"ca-central-1", "eu-west-1", "eu-west-2", "eu-west-3",
		"eu-central-1", "eu-north-1", "eu-south-1",
		"ap-southeast-1", "ap-southeast-2", "ap-northeast-1",
		"ap-northeast-2", "ap-northeast-3", "ap-south-1",
		"sa-east-1", "me-south-1", "af-south-1",
	}

	cliAzureLocations = []string{
		"eastus", "eastus2", "westus2", "westus3",
		"centralus", "northcentralus", "southcentralus",
		"westeurope", "northeurope", "uksouth", "ukwest",
		"germanywestcentral", "swedencentral", "switzerlandnorth",
		"japaneast", "japanwest", "australiaeast",
		"southeastasia", "eastasia",
		"brazilsouth",
		"canadacentral", "canadaeast",
	}

	cliAWSNodeTypes = []string{
		"c5.2xlarge", "c5.4xlarge",
		"m5.2xlarge", "m5.4xlarge",
		"r5.2xlarge",
	}

	cliAzureVMSizes = []string{
		"Standard_D4s_v5", "Standard_D8s_v5",
		"Standard_D4s_v3", "Standard_D8s_v3",
		"Standard_E4s_v5", "Standard_E8s_v5",
		"Standard_F8s_v2",
	}
)

const wizardSrc = "interactive"

// runInstallWizard walks the user through install settings and writes them onto cfg.
// Returns false if the user aborts mid-wizard.
func runInstallWizard(reader *bufio.Reader, cfg *config.Config) bool {
	fmt.Println()
	fmt.Println("Install configuration")
	fmt.Println("  Press Enter to keep the value in [brackets].")
	fmt.Println("  Type q at any prompt to cancel.")
	fmt.Println()

	// ── 1. Cloud & cluster basics ────────────────────────────────────────────
	fmt.Println("── 1/4  Cluster basics ──")
	cloud, ok := promptChoice(reader, "Cloud provider", []string{"aws", "azure"}, cfg.Cloud)
	if !ok {
		return false
	}
	cfg.Cloud = cloud

	nameDefault := cfg.ClusterName.Value
	if nameDefault == "" {
		nameDefault = "was-prod"
	}
	for {
		name, ok := promptString(reader, "Cluster name", nameDefault)
		if !ok {
			return false
		}
		if !cliClusterNameRe.MatchString(name) {
			fmt.Println("  Invalid name: letters, digits, hyphens; must start with a letter.")
			continue
		}
		cfg.ClusterName = config.Field[string]{Value: name, Source: wizardSrc}
		break
	}

	k8sChoices := versions.K8sInstallChoices(versions.DefaultClusterK8s(cloud))
	k8sDefault := cfg.K8sVersion.Value
	if k8sDefault == "" || !containsStr(k8sChoices, k8sDefault) {
		k8sDefault = versions.DefaultClusterK8s(cloud)
	}
	k8s, ok := promptChoice(reader, "Kubernetes version", k8sChoices, k8sDefault)
	if !ok {
		return false
	}
	cfg.K8sVersion = config.Field[string]{Value: k8s, Source: wizardSrc}

	if cloud == "azure" {
		locDefault := cfg.AzureLocation.Value
		if locDefault == "" {
			locDefault = "eastus"
		}
		loc, ok := promptChoiceOrCustom(reader, "Azure location", cliAzureLocations, locDefault)
		if !ok {
			return false
		}
		cfg.AzureLocation = config.Field[string]{Value: loc, Source: wizardSrc}
	} else {
		regDefault := cfg.Region.Value
		if regDefault == "" {
			regDefault = "us-east-1"
		}
		reg, ok := promptChoiceOrCustom(reader, "AWS region", cliEKSRegions, regDefault)
		if !ok {
			return false
		}
		cfg.Region = config.Field[string]{Value: reg, Source: wizardSrc}
	}

	// ── 2. Networking ────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("── 2/4  Networking ──")
	fmt.Println("  Ingress hostname is optional. Leave blank to use the load-balancer hostname later.")
	for {
		host, ok := promptString(reader, "Ingress hostname (optional)", cfg.IngressHost.Value)
		if !ok {
			return false
		}
		if host == "" {
			cfg.IngressHost = config.Field[string]{Value: "", Source: "unset"}
			break
		}
		if !tools.IsValidIngressHost(host) {
			fmt.Println("  Invalid hostname (DNS name required; not a raw IP on Azure).")
			continue
		}
		cfg.IngressHost = config.Field[string]{Value: host, Source: wizardSrc}
		break
	}

	// ── 3. Capacity ──────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("── 3/4  Node pool ──")
	var nodeChoices []string
	nodeDefault := cfg.NodeType.Value
	if cloud == "azure" {
		nodeChoices = cliAzureVMSizes
		if nodeDefault == "" || !containsStr(nodeChoices, nodeDefault) {
			nodeDefault = "Standard_D4s_v5"
		}
	} else {
		nodeChoices = cliAWSNodeTypes
		if nodeDefault == "" || !containsStr(nodeChoices, nodeDefault) {
			nodeDefault = "c5.2xlarge"
		}
	}
	nodeType, ok := promptChoiceOrCustom(reader, "Node instance type", nodeChoices, nodeDefault)
	if !ok {
		return false
	}
	cfg.NodeType = config.Field[string]{Value: nodeType, Source: wizardSrc}

	for {
		minS, ok := promptString(reader, "Min nodes", orDefault(cfg.NodeMin.Value, "2"))
		if !ok {
			return false
		}
		desS, ok := promptString(reader, "Desired nodes", orDefault(cfg.NodeDesired.Value, "2"))
		if !ok {
			return false
		}
		maxS, ok := promptString(reader, "Max nodes", orDefault(cfg.NodeMax.Value, "10"))
		if !ok {
			return false
		}
		min, minErr := strconv.Atoi(minS)
		des, desErr := strconv.Atoi(desS)
		max, maxErr := strconv.Atoi(maxS)
		if minErr != nil || desErr != nil || maxErr != nil || min < 1 || des < 1 || max < 1 {
			fmt.Println("  Node counts must be integers ≥ 1.")
			continue
		}
		if des < min {
			fmt.Println("  Desired must be ≥ min.")
			continue
		}
		if max < des {
			fmt.Println("  Max must be ≥ desired.")
			continue
		}
		cfg.NodeMin = config.Field[string]{Value: minS, Source: wizardSrc}
		cfg.NodeDesired = config.Field[string]{Value: desS, Source: wizardSrc}
		cfg.NodeMax = config.Field[string]{Value: maxS, Source: wizardSrc}
		break
	}

	// ── 4. Optional add-ons ──────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("── 4/4  Optional add-ons ──")
	fmt.Println("  Core add-ons (ingress-nginx, CSI/StorageClass, Strimzi when Kafka is on) are always installed.")
	fmt.Println("  Uncheck optional components you manage yourself or do not need.")
	fmt.Println()

	certManagerDefault := cfg.Cloud != "aws"
	if cfg.Cloud == "aws" {
		fmt.Println("  Note (AWS): cert-manager / Let's Encrypt only works with a custom DNS name")
		fmt.Println("  (e.g. was.example.com → CNAME to the ELB). Default *.elb.amazonaws.com cannot get TLS.")
		fmt.Println("  Azure keeps the default on — *.cloudapp.azure.com works with Let's Encrypt.")
		fmt.Println()
	}
	certManager, ok := promptYesNo(reader, "Install cert-manager (TLS / Let's Encrypt)", certManagerDefault)
	if !ok {
		return false
	}
	prometheus, ok := promptYesNo(reader, "Install Prometheus + prometheus-adapter", true)
	if !ok {
		return false
	}
	metricsServer, ok := promptYesNo(reader, "Install metrics-server", true)
	if !ok {
		return false
	}
	kafka, ok := promptYesNo(reader, "Install Strimzi Kafka (recommended)", true)
	if !ok {
		return false
	}

	var skips []string
	if !certManager {
		skips = append(skips, "cert-manager")
	}
	if !prometheus {
		skips = append(skips, "kube-prometheus-stack", "prometheus-adapter")
	}
	if !metricsServer {
		skips = append(skips, "metrics-server")
	}

	kafkaMode := "builtin"
	kafkaBootstrap := ""
	if !kafka {
		skips = append(skips, "strimzi-kafka-operator")
		kafkaMode = "external"
		fmt.Println("  External Kafka requires a bootstrap host:port.")
		for {
			host, ok := promptString(reader, "Kafka bootstrap host", "")
			if !ok {
				return false
			}
			port, ok := promptString(reader, "Kafka bootstrap port", "9092")
			if !ok {
				return false
			}
			if host == "" || port == "" {
				fmt.Println("  Host and port are required when Strimzi Kafka is skipped.")
				continue
			}
			kafkaBootstrap = host + ":" + port
			break
		}
	}

	cfg.AddonsSkip = config.Field[string]{Value: strings.Join(skips, ","), Source: wizardSrc}
	cfg.KafkaMode = config.Field[string]{Value: kafkaMode, Source: wizardSrc}
	cfg.KafkaBootstrapServers = config.Field[string]{Value: kafkaBootstrap, Source: wizardSrc}

	return true
}

// ── Prompt helpers ────────────────────────────────────────────────────────────

func promptString(reader *bufio.Reader, label, def string) (string, bool) {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if strings.EqualFold(line, "q") {
		return "", false
	}
	if line == "" {
		return def, true
	}
	return line, true
}

func promptYesNo(reader *bufio.Reader, label string, defYes bool) (bool, bool) {
	hint := "Y/n"
	if !defYes {
		hint = "y/N"
	}
	fmt.Printf("%s [%s]: ", label, hint)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "q" {
		return false, false
	}
	if line == "" {
		return defYes, true
	}
	if line == "y" || line == "yes" {
		return true, true
	}
	if line == "n" || line == "no" {
		return false, true
	}
	fmt.Println("  Please answer y or n.")
	return promptYesNo(reader, label, defYes)
}

func promptChoice(reader *bufio.Reader, label string, choices []string, def string) (string, bool) {
	fmt.Printf("%s — options: %s\n", label, strings.Join(choices, ", "))
	for {
		val, ok := promptString(reader, label, def)
		if !ok {
			return "", false
		}
		val = strings.ToLower(val)
		for _, c := range choices {
			if strings.EqualFold(c, val) {
				return c, true
			}
		}
		fmt.Printf("  Choose one of: %s\n", strings.Join(choices, ", "))
	}
}

func promptChoiceOrCustom(reader *bufio.Reader, label string, choices []string, def string) (string, bool) {
	fmt.Printf("%s — common: %s\n", label, strings.Join(choices, ", "))
	fmt.Println("  (Enter a listed value, or type any other valid value.)")
	return promptString(reader, label, def)
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
