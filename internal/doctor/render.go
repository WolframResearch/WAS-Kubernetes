package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// ── Text renderer ─────────────────────────────────────────────────────────────

// TextRenderer writes human-readable doctor output. Color is emitted when w
// is a real TTY.
type TextRenderer struct {
	w     io.Writer
	color bool
}

// NewTextRenderer returns a TextRenderer for w.
func NewTextRenderer(w io.Writer) *TextRenderer {
	color := false
	if f, ok := w.(*os.File); ok {
		color = term.IsTerminal(int(f.Fd()))
	}
	return &TextRenderer{w: w, color: color}
}

const (
	iconPass  = "✓"
	iconFail  = "✗"
	iconSkip  = "↷"
	iconError = "!"
	iconInfo  = "·"

	ansiReset    = "\x1b[0m"
	ansiGreen    = "\x1b[32m"
	ansiRed      = "\x1b[31m"
	ansiYellow   = "\x1b[33m"
	ansiDim      = "\x1b[2m"
	ansiCyan     = "\x1b[36m"
	ansiBold     = "\x1b[1m"
)

func (r *TextRenderer) col(s, code string) string {
	if !r.color {
		return s
	}
	return code + s + ansiReset
}

// Render writes the full report to r.w.
func (r *TextRenderer) Render(rep *Report, verbose bool) {
	categories := []Category{CategoryEnvironment, CategoryCluster, CategoryApplication}
	catFindings := map[Category][]Finding{}
	for _, f := range rep.Findings {
		cat := categoryOfID(f.CheckID)
		catFindings[cat] = append(catFindings[cat], f)
	}

	for _, cat := range categories {
		findings := catFindings[cat]
		if len(findings) == 0 {
			continue
		}
		fmt.Fprintf(r.w, "\n%s\n", r.col(cat.String(), ansiBold+ansiCyan))
		for _, f := range findings {
			r.renderFinding(f, verbose)
		}
	}

	r.renderSummary(rep)
}

func (r *TextRenderer) renderFinding(f Finding, verbose bool) {
	icon, iconColor := r.statusIcon(f)
	elapsed := ""
	if f.Elapsed > 0 {
		elapsed = r.col(fmt.Sprintf("  %s", fmtElapsed(f.Elapsed)), ansiDim)
	}

	name := f.CheckName
	if f.Status == StatusFail {
		name = r.col(name, ansiBold)
	}

	fmt.Fprintf(r.w, "  %s  %s%s\n", r.col(icon, iconColor), name, elapsed)

	if f.Status == StatusFail || f.Status == StatusError {
		if f.Message != "" {
			fmt.Fprintf(r.w, "     %s\n", r.col(f.Message, ansiRed))
		}
		if verbose && f.Remediation != "" {
			for _, line := range strings.Split(f.Remediation, "\n") {
				fmt.Fprintf(r.w, "     %s\n", r.col(line, ansiDim))
			}
		} else if !verbose && f.Remediation != "" {
			fmt.Fprintf(r.w, "     %s\n", r.col("(pass --verbose for remediation steps)", ansiDim))
		}
	} else if f.Status == StatusSkip && f.Message != "" {
		fmt.Fprintf(r.w, "     %s\n", r.col(f.Message, ansiDim))
	}
}

func (r *TextRenderer) statusIcon(f Finding) (string, string) {
	switch f.Status {
	case StatusPass:
		return iconPass, ansiGreen
	case StatusFail:
		switch f.Severity {
		case SeverityCritical, SeverityProblem:
			return iconFail, ansiRed
		default:
			return iconFail, ansiYellow
		}
	case StatusSkip:
		return iconSkip, ansiDim
	case StatusError:
		return iconError, ansiRed
	default:
		return iconInfo, ansiDim
	}
}

func (r *TextRenderer) renderSummary(rep *Report) {
	s := rep.Summary
	parts := []string{}
	if s.Fail > 0 {
		critCount := 0
		warnCount := 0
		probCount := 0
		for _, f := range rep.Findings {
			if f.Status != StatusFail {
				continue
			}
			switch f.Severity {
			case SeverityCritical:
				critCount++
			case SeverityProblem:
				probCount++
			case SeverityWarning:
				warnCount++
			}
		}
		if critCount > 0 {
			parts = append(parts, r.col(fmt.Sprintf("%d critical", critCount), ansiRed))
		}
		if probCount > 0 {
			parts = append(parts, r.col(fmt.Sprintf("%d problem", probCount), ansiRed))
		}
		if warnCount > 0 {
			parts = append(parts, r.col(fmt.Sprintf("%d warning", warnCount), ansiYellow))
		}
	}
	if s.Pass > 0 {
		parts = append(parts, r.col(fmt.Sprintf("%d pass", s.Pass), ansiGreen))
	}
	if s.Skip > 0 {
		parts = append(parts, r.col(fmt.Sprintf("%d skip", s.Skip), ansiDim))
	}
	if s.Error > 0 {
		parts = append(parts, r.col(fmt.Sprintf("%d error", s.Error), ansiRed))
	}

	fmt.Fprintf(r.w, "\n%s  %s  (elapsed: %s)\n\n",
		r.col("Summary", ansiBold),
		strings.Join(parts, " · "),
		fmtElapsed(rep.Elapsed),
	)
}

func fmtElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// ── JSON renderer ─────────────────────────────────────────────────────────────

// JSONRenderer emits a stable JSON report.
type JSONRenderer struct{ w io.Writer }

// NewJSONRenderer returns a JSONRenderer for w.
func NewJSONRenderer(w io.Writer) *JSONRenderer { return &JSONRenderer{w: w} }

type jsonReport struct {
	SchemaVersion string        `json:"schemaVersion"`
	GeneratedAt   time.Time     `json:"generatedAt"`
	Cluster       string        `json:"cluster"`
	WasctlVersion string        `json:"wasctlVersion"`
	ElapsedMs     int64         `json:"elapsedMs"`
	Summary       jsonSummary   `json:"summary"`
	Findings      []jsonFinding `json:"findings"`
}

type jsonSummary struct {
	Pass  int `json:"pass"`
	Fail  int `json:"fail"`
	Skip  int `json:"skip"`
	Error int `json:"error"`
}

type jsonFinding struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	Details     string `json:"details,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	DocsLink    string `json:"docsLink,omitempty"`
	ElapsedMs   int64  `json:"elapsedMs"`
}

// Render writes JSON to r.w.
func (r *JSONRenderer) Render(rep *Report) error {
	out := jsonReport{
		SchemaVersion: "1",
		GeneratedAt:   rep.GeneratedAt,
		Cluster:       rep.Cluster,
		WasctlVersion: rep.WasctlVersion,
		ElapsedMs:     rep.Elapsed.Milliseconds(),
		Summary: jsonSummary{
			Pass:  rep.Summary.Pass,
			Fail:  rep.Summary.Fail,
			Skip:  rep.Summary.Skip,
			Error: rep.Summary.Error,
		},
	}
	for _, f := range rep.Findings {
		out.Findings = append(out.Findings, jsonFinding{
			ID:          f.CheckID,
			Name:        f.CheckName,
			Category:    categoryOfID(f.CheckID).String(),
			Severity:    f.Severity.String(),
			Status:      statusString(f.Status),
			Message:     f.Message,
			Details:     f.Details,
			Remediation: f.Remediation,
			DocsLink:    f.DocsLink,
			ElapsedMs:   f.Elapsed.Milliseconds(),
		})
	}
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func statusString(s Status) string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusFail:
		return "fail"
	case StatusSkip:
		return "skip"
	case StatusError:
		return "error"
	default:
		return "info"
	}
}

func categoryOfID(id string) Category {
	switch {
	case strings.HasPrefix(id, "cluster."):
		return CategoryCluster
	case strings.HasPrefix(id, "app.") || strings.HasPrefix(id, "chart."):
		return CategoryApplication
	default:
		return CategoryEnvironment
	}
}

// ── Markdown renderer ─────────────────────────────────────────────────────────

// MarkdownRenderer emits markdown suitable for pasting into support tickets.
// Account IDs are partially masked to avoid leaking them into issue trackers.
type MarkdownRenderer struct{ w io.Writer }

// NewMarkdownRenderer returns a MarkdownRenderer for w.
func NewMarkdownRenderer(w io.Writer) *MarkdownRenderer { return &MarkdownRenderer{w: w} }

// Render writes markdown to r.w.
func (r *MarkdownRenderer) Render(rep *Report) {
	fmt.Fprintf(r.w, "## wasctl doctor — %s — %s\n\n",
		rep.Cluster, rep.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(r.w, "wasctl version: `%s`  |  elapsed: %s\n\n",
		rep.WasctlVersion, fmtElapsed(rep.Elapsed))

	categories := []Category{CategoryEnvironment, CategoryCluster, CategoryApplication}
	catFindings := map[Category][]Finding{}
	for _, f := range rep.Findings {
		cat := categoryOfID(f.CheckID)
		catFindings[cat] = append(catFindings[cat], f)
	}

	for _, cat := range categories {
		findings := catFindings[cat]
		if len(findings) == 0 {
			continue
		}
		fmt.Fprintf(r.w, "### %s\n\n", cat.String())
		fmt.Fprintf(r.w, "| Status | Check | Details | Elapsed |\n")
		fmt.Fprintf(r.w, "|--------|-------|---------|--------|\n")
		for _, f := range findings {
			badge := mdBadge(f)
			msg := sanitize(f.Message)
			fmt.Fprintf(r.w, "| %s | %s | %s | %s |\n",
				badge, f.CheckName, msg, fmtElapsed(f.Elapsed))
		}
		fmt.Fprintln(r.w)

		// Expanded details for failures
		for _, f := range findings {
			if f.Status != StatusFail || f.Details == "" {
				continue
			}
			fmt.Fprintf(r.w, "<details><summary>%s — details</summary>\n\n", f.CheckName)
			fmt.Fprintf(r.w, "```\n%s\n```\n\n", sanitize(f.Details))
			if f.Remediation != "" {
				fmt.Fprintf(r.w, "**Remediation:** %s\n\n", f.Remediation)
			}
			fmt.Fprintf(r.w, "</details>\n\n")
		}
	}

	s := rep.Summary
	fmt.Fprintf(r.w, "### Summary\n\n%d pass · %d fail · %d skip · %d error\n",
		s.Pass, s.Fail, s.Skip, s.Error)
}

func mdBadge(f Finding) string {
	switch f.Status {
	case StatusPass:
		return "✅ Pass"
	case StatusFail:
		switch f.Severity {
		case SeverityCritical:
			return "🔴 Critical"
		case SeverityProblem:
			return "❌ Problem"
		default:
			return "⚠️ Warning"
		}
	case StatusSkip:
		return "⏭ Skip"
	case StatusError:
		return "🔧 Error"
	default:
		return "ℹ️ Info"
	}
}

// sanitize partially masks 12-digit AWS account IDs (e.g. 123456789012 → 1234****9012).
func sanitize(s string) string {
	// Replace full 12-digit account IDs with masked form
	out := strings.Builder{}
	i := 0
	for i < len(s) {
		// Look for a run of 12 consecutive digits
		if i+12 <= len(s) && isDigits(s[i:i+12]) {
			masked := s[i:i+4] + "****" + s[i+8:i+12]
			out.WriteString(masked)
			i += 12
		} else {
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
