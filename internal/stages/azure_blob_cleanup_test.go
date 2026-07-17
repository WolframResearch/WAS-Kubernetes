package stages

import (
	"context"
	"strings"
	"testing"
)

type blobCleanupReporter struct {
	lines []string
}

func (r *blobCleanupReporter) SubstepStart(string)    {}
func (r *blobCleanupReporter) SubstepDone()           {}
func (r *blobCleanupReporter) SubstepFail(error)      {}
func (r *blobCleanupReporter) LogLine(line string)    { r.lines = append(r.lines, line) }

func TestEmptyAzureBlobContainerRequiresArgs(t *testing.T) {
	rep := &blobCleanupReporter{}
	err := emptyAzureBlobContainer(context.Background(), "", "c", "k", rep)
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected required-args error, got %v", err)
	}
}
