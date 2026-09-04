// Package handlers contains HTTP handler functions for the wasctl web UI.
package handlers

import (
	"context"
	"html/template"
)

// getAWSAccountID retrieves the AWS Account ID. It checks the serverCredCache
// first, falling back to GetCallerIdentityFnForTest (which can be overridden in tests).
func getAWSAccountID(ctx context.Context, region string) (string, error) {
	if serverCredCache != nil {
		if avail, ok := serverCredCache.getAWS(region); ok && avail.Authenticated {
			return avail.ActiveAccount, nil
		}
	}
	id, err := GetCallerIdentityFnForTest(ctx, region)
	if err != nil {
		return "", err
	}
	if serverCredCache != nil {
		// Populate cache entry
		serverCredCache.setAWS(region, CloudAvailability{
			CLIInstalled:  true,
			Authenticated: true,
			ActiveAccount: id.Account,
		})
	}
	return id.Account, nil
}

// Templates holds one parsed template per page. Each is an independent clone
// of the base layout so page-level {{define "content"}} blocks don't conflict.
type Templates struct {
	Home           *template.Template
	About          *template.Template
	Cluster        *template.Template
	InstallWizard  *template.Template
	InstallPreview *template.Template
	InstallStream  *template.Template
	ChartOnlyLanding  *template.Template
	ChartValuesEditor *template.Template
	ChartPreview      *template.Template
	ChartApply        *template.Template
	DestroyConfirm *template.Template
	DestroyStream  *template.Template
	OpsOverview      *template.Template
	OpsDoctor        *template.Template
	OpsDoctorRunning *template.Template
	OpsBundle        *template.Template
	OpsVersions      *template.Template
	OpsHistory       *template.Template
}

// renderErr writes a plain-text 500 response. Used when template execution
// itself fails — at that point the response headers may or may not be sent.
func renderErr(w interface{ Write([]byte) (int, error) }, err error) {
	_, _ = w.Write([]byte("internal error: " + err.Error()))
}
