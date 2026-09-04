package handlers

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/audit"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/inspect"
)

// FuncMap returns the template.FuncMap used by all pages.
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"statusClass":      statusClassFn,
		"addonStatusClass": addonStatusClassFn,
		"maskAccount":      maskAccountFn,
		"formatBytes":      formatBytesFn,
		"sectionErrors":    sectionErrorsFn,
		"replace":          strings.ReplaceAll,
		"auditAge":         audit.FormatAge,
		"auditResultClass": auditResultClassFn,
		// Operations console
		"findingClass": findingClassFn,
		"findingIcon":  findingIconFn,
		"fmtElapsed":   fmtElapsedFn,
	}
}

func statusClassFn(s string) string {
	switch strings.ToLower(s) {
	case "active", "deployed", "running":
		return "deployed"
	case "installing", "pending", "updating":
		return "installing"
	case "destroyed", "deleted":
		return "destroyed"
	case "error", "failed", "unhealthy":
		return "error"
	default:
		return "active"
	}
}

func addonStatusClassFn(s string) string {
	switch strings.ToLower(s) {
	case "deployed":
		return "deployed"
	case "pending-install", "pending-upgrade":
		return "installing"
	case "failed", "error":
		return "error"
	default:
		return "active"
	}
}

func maskAccountFn(id string) string {
	if len(id) <= 4 {
		return id
	}
	masked := strings.Repeat("*", len(id)-4) + id[len(id)-4:]
	return masked
}

func formatBytesFn(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.0f KB", float64(b)/KB)
	case b > 0:
		return fmt.Sprintf("%d B", b)
	default:
		return "—"
	}
}

// findingClassFn maps a doctor Status+Severity pair to a CSS modifier class.
func findingClassFn(status doctor.Status, sev doctor.Severity) string {
	if status != doctor.StatusFail {
		switch status {
		case doctor.StatusPass:
			return "pass"
		case doctor.StatusSkip:
			return "skip"
		case doctor.StatusInfo:
			return "info"
		default:
			return "error"
		}
	}
	switch sev {
	case doctor.SeverityCritical:
		return "critical"
	case doctor.SeverityProblem:
		return "problem"
	case doctor.SeverityWarning:
		return "warning"
	default:
		return "fail"
	}
}

// findingIconFn returns the brand-pattern status icon for a finding.
func findingIconFn(status doctor.Status, sev doctor.Severity) template.HTML {
	if status != doctor.StatusFail {
		switch status {
		case doctor.StatusPass:
			return template.HTML(`<span class="icon icon--pass">[✓]</span>`)
		case doctor.StatusSkip:
			return template.HTML(`<span class="icon icon--skip">[–]</span>`)
		case doctor.StatusInfo:
			return template.HTML(`<span class="icon icon--info">[i]</span>`)
		default:
			return template.HTML(`<span class="icon icon--error">[?]</span>`)
		}
	}
	switch sev {
	case doctor.SeverityCritical:
		return template.HTML(`<span class="icon icon--critical">▲</span>`)
	case doctor.SeverityProblem:
		return template.HTML(`<span class="icon icon--problem">[!]</span>`)
	case doctor.SeverityWarning:
		return template.HTML(`<span class="icon icon--warning">[~]</span>`)
	default:
		return template.HTML(`<span class="icon icon--fail">[!]</span>`)
	}
}

// fmtElapsedFn formats a time.Duration as "2.1s" or "123ms".
func fmtElapsedFn(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// auditResultClassFn maps an audit entry result string to a CSS modifier class.
// The result field contains "success", "failed: ...", or "error: ..." from the
// destroy/install goroutines, so we match by prefix rather than exact string.
func auditResultClassFn(result string) string {
	lower := strings.ToLower(result)
	switch {
	case lower == "success":
		return "success"
	case strings.HasPrefix(lower, "failed"):
		return "failed"
	case strings.HasPrefix(lower, "error"):
		return "error"
	default:
		return ""
	}
}

// sectionErrorsFn renders a small error note if the report has an error for
// the named section. Returns an empty HTML string if no error.
func sectionErrorsFn(r *inspect.Report, section string) template.HTML {
	if r == nil {
		return ""
	}
	for _, e := range r.Errors {
		if strings.EqualFold(e.Section, section) {
			return template.HTML(
				`<p class="section-error text-sm text-muted">` +
					template.HTMLEscapeString(e.Section) + ` unavailable: ` +
					template.HTMLEscapeString(e.Error) + `</p>`,
			)
		}
	}
	return ""
}
