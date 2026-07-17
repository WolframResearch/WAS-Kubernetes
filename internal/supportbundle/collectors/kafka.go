package collectors

import (
	"context"
	"fmt"
)

const kafkaNamespace = "kafka"

// KafkaCollector collects Strimzi Kafka CRs and operator status.
type KafkaCollector struct{}

func (KafkaCollector) Name() string { return "kafka" }

func (KafkaCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	if cc.Kubeconfig == "" {
		return nil, fmt.Errorf("no cluster access (kubeconfig unavailable)")
	}

	kargs := kubectlArgs(cc)
	ns := []string{"-n", kafkaNamespace}
	var files []File

	kafkaCR, err := runOutput(ctx, "kubectl",
		append(kargs, append([]string{"get", "kafka", "-o", "json"}, ns...)...)...)
	if err != nil {
		kafkaCR = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "kafka/kafka_cr.json", Content: kafkaCR})

	topics, err := runOutput(ctx, "kubectl",
		append(kargs, append([]string{"get", "kafkatopic", "-o", "json"}, ns...)...)...)
	if err != nil {
		topics = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "kafka/kafka_topics.json", Content: topics})

	bridge, err := runOutput(ctx, "kubectl",
		append(kargs, append([]string{"get", "kafkabridge", "-o", "json"}, ns...)...)...)
	if err != nil {
		bridge = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "kafka/kafka_bridge.json", Content: bridge})

	// Strimzi operator deployment.
	operator, err := runOutput(ctx, "kubectl",
		append(kargs, "get", "deployment", "strimzi-cluster-operator", "-n", kafkaNamespace, "-o", "json")...)
	if err != nil {
		operator = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "kafka/strimzi_operator.json", Content: operator})

	return files, nil
}
