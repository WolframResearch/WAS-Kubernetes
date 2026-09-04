// Package runner executes subprocesses and streams their stdout+stderr
// line-by-line to a Reporter.
package runner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// ansiRe matches ANSI/VT100 escape sequences so they can be stripped from
// subprocess output before forwarding to reporters and error messages.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// Reporter receives output from a running subprocess. Both the TUI and
// plain-mode implementations satisfy this interface.
type Reporter interface {
	SubstepStart(name string)
	SubstepDone()
	SubstepFail(err error)
	LogLine(line string)
}

// Runner executes a command and streams its output to a Reporter.
type Runner interface {
	Run(ctx context.Context, cmd []string, env []string, r Reporter) error
	// Output runs cmd and returns combined stdout+stderr as bytes. No Reporter
	// — use this when the output needs to be parsed (e.g. helm status -o json).
	Output(ctx context.Context, cmd []string, env []string) ([]byte, error)
}

// ExecRunner is the production Runner. It forks the given command, captures
// combined stdout+stderr, and calls r.LogLine for every line received.
type ExecRunner struct{}

// tailLines is the number of output lines preserved in the error on failure.
const tailLines = 30

// Run executes cmd[0] with cmd[1:] as arguments. env entries ("KEY=VALUE")
// are appended to the current process environment (pass nil to inherit only).
// The subprocess is killed when ctx is cancelled.
//
// On failure the returned error includes the last tailLines lines of output so
// the cause is visible even when the live log (TUI or plain) is no longer on
// screen.
func (ExecRunner) Run(ctx context.Context, cmd []string, env []string, r Reporter) error {
	if len(cmd) == 0 {
		return fmt.Errorf("runner: empty command")
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("runner: pipe: %w", err)
	}

	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...) //nolint:gosec
	c.Stdout = pw
	c.Stderr = pw
	if len(env) > 0 {
		c.Env = append(os.Environ(), env...)
	}

	if err := c.Start(); err != nil {
		pw.Close()
		pr.Close()
		return fmt.Errorf("runner: start %q: %w", cmd[0], err)
	}
	// Close the write-end in the parent so the scanner sees EOF when the
	// child exits.
	pw.Close()

	var tail []string
	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		line := ansiRe.ReplaceAllString(scanner.Text(), "")
		r.LogLine(line)
		tail = append(tail, line)
		if len(tail) > tailLines {
			tail = tail[1:]
		}
	}
	pr.Close()

	if err := c.Wait(); err != nil {
		// Use only binary + first meaningful arg (e.g. "terraform apply") to
		// keep the error header short — full args are in the log above.
		label := cmd[0]
		for _, a := range cmd[1:] {
			if !strings.HasPrefix(a, "-") {
				label += " " + a
				break
			}
		}
		if len(tail) > 0 {
			return fmt.Errorf("%s failed:\n%s", label, strings.Join(tail, "\n"))
		}
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

// Output runs cmd and returns combined stdout+stderr. ANSI sequences are
// stripped so callers parsing JSON or plain text don't encounter escape codes.
func (ExecRunner) Output(ctx context.Context, cmd []string, env []string) ([]byte, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("runner: empty command")
	}
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...) //nolint:gosec
	if len(env) > 0 {
		c.Env = append(os.Environ(), env...)
	}
	out, err := c.CombinedOutput()
	out = bytes.ReplaceAll(ansiRe.ReplaceAll(out, nil), []byte("\r"), nil)
	if err != nil {
		return out, fmt.Errorf("%s: %w", cmd[0], err)
	}
	return out, nil
}
