package runner

import (
	"context"
	"fmt"
	"strings"
)

// MockRunner is a test double for Runner. It matches commands against a list of
// registered rules and returns the configured lines and exit code for each.
//
// Usage in tests:
//
//	m := runner.NewMock()
//	m.Register("terraform init", []string{"Initializing..."})
//	m.RegisterError("helm upgrade", []string{"Error: ..."}, errors.New("exit 1"))
//	m.RegisterOutput("helm status", []byte(`{"info":{"status":"deployed"}}`), nil)
type MockRunner struct {
	rules       []mockRule
	outputRules []outputRule
	Calls       [][]string // all commands executed, in order
}

type mockRule struct {
	prefix string
	lines  []string
	err    error
}

type outputRule struct {
	prefix string
	data   []byte
	err    error
	// queue, when non-nil, returns one result per call (FIFO) for this prefix.
	queue []struct {
		data []byte
		err  error
	}
}

// NewMock returns an empty MockRunner.
func NewMock() *MockRunner { return &MockRunner{} }

// Register adds a rule: when a command starts with prefix, emit lines and
// return nil. Rules are matched in registration order; first match wins.
func (m *MockRunner) Register(prefix string, lines []string) {
	m.rules = append(m.rules, mockRule{prefix: prefix, lines: lines, err: nil})
}

// RegisterError adds a rule that emits lines then returns err.
func (m *MockRunner) RegisterError(prefix string, lines []string, err error) {
	m.rules = append(m.rules, mockRule{prefix: prefix, lines: lines, err: err})
}

// Run records the call, matches it against registered rules, emits lines to
// r.LogLine, then returns the configured error (or an "unexpected command"
// error if nothing matched).
func (m *MockRunner) Run(_ context.Context, cmd []string, _ []string, r Reporter) error {
	m.Calls = append(m.Calls, cmd)
	joined := strings.Join(cmd, " ")

	for _, rule := range m.rules {
		if strings.HasPrefix(joined, rule.prefix) {
			for _, line := range rule.lines {
				r.LogLine(line)
			}
			return rule.err
		}
	}
	return fmt.Errorf("MockRunner: unexpected command: %s", joined)
}

// RegisterOutput adds a rule: when Output() is called with a command starting
// with prefix, return data and err.
func (m *MockRunner) RegisterOutput(prefix string, data []byte, err error) {
	m.outputRules = append(m.outputRules, outputRule{prefix: prefix, data: data, err: err})
}

// OutputResponse is one FIFO entry for RegisterOutputSequence.
type OutputResponse struct {
	Data []byte
	Err  error
}

// RegisterOutputSequence registers FIFO responses for a command prefix.
// Each Output() match consumes the next entry until one remains, which is reused.
func (m *MockRunner) RegisterOutputSequence(prefix string, responses []OutputResponse) {
	q := make([]struct {
		data []byte
		err  error
	}, len(responses))
	for i, r := range responses {
		q[i] = struct {
			data []byte
			err  error
		}{r.Data, r.Err}
	}
	m.outputRules = append(m.outputRules, outputRule{prefix: prefix, queue: q})
}

// Output records the call, matches against registered output rules, then returns
// the configured data. Falls back to an error if nothing matched.
func (m *MockRunner) Output(_ context.Context, cmd []string, _ []string) ([]byte, error) {
	m.Calls = append(m.Calls, cmd)
	joined := strings.Join(cmd, " ")
	for i := range m.outputRules {
		rule := &m.outputRules[i]
		if !strings.HasPrefix(joined, rule.prefix) {
			continue
		}
		if len(rule.queue) > 0 {
			item := rule.queue[0]
			if len(rule.queue) > 1 {
				rule.queue = rule.queue[1:]
			}
			return item.data, item.err
		}
		return rule.data, rule.err
	}
	return nil, fmt.Errorf("MockRunner: unexpected Output command: %s", joined)
}

// CalledWith returns true if any recorded call's joined string contains substr.
func (m *MockRunner) CalledWith(substr string) bool {
	for _, c := range m.Calls {
		if strings.Contains(strings.Join(c, " "), substr) {
			return true
		}
	}
	return false
}
