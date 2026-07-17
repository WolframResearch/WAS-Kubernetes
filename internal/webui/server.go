// Package webui provides the wasctl HTTP server (wasctl serve).
package webui

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/handlers"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/schema"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/sse"
)

// Server is an HTTP server that serves the wasctl web UI.
type Server struct {
	addr        string
	metaRegion  string
	region      string
	chartDir    string
	localMode   bool
	repoRoot    string
	broker      *sse.Broker
	schema      *schema.Schema
	mux         *http.ServeMux
	tmpls       *handlers.Templates
	bundleStore *handlers.BundleStore
}

// NewServer creates a Server. It parses all embedded templates once at startup
// and returns an error if any template fails to parse.
//
// chartDir is the path to the WAS Helm chart directory.
// localMode and repoRoot control the in-browser development banner shown when
// wasctl is started with --local.
func NewServer(addr, metaRegion, region, chartDir string, localMode bool, repoRoot string) (*Server, error) {
	tmpls, err := parseTemplates(localMode, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	sch, err := schema.Load(FS, "static/values.schema.json")
	if err != nil {
		return nil, fmt.Errorf("load schema: %w", err)
	}

	bs := &handlers.BundleStore{}
	go bs.StartGC()

	// Cache credential detection results for 30s. Avoids forking "aws --version"
	// and "aws sts get-caller-identity" on every HTTP request (including 10s polls).
	handlers.EnableCredentialCache(30 * time.Second)

	s := &Server{
		addr:        addr,
		metaRegion:  metaRegion,
		region:      region,
		chartDir:    chartDir,
		localMode:   localMode,
		repoRoot:    repoRoot,
		broker:      sse.NewBroker(),
		schema:      sch,
		tmpls:       tmpls,
		bundleStore: bs,
	}
	s.mux = http.NewServeMux()
	s.registerRoutes()
	return s, nil
}

// Handler returns the underlying http.Handler. The caller owns the server
// lifecycle (ListenAndServe, Shutdown).
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) registerRoutes() {
	// Static assets — CSS, JS, SVGs
	staticFS, _ := fs.Sub(FS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	// Page routes
	s.mux.HandleFunc("GET /{$}", handlers.Home(s.tmpls, s.metaRegion, s.region))
	// HTMX partial: tab switch on home page
	s.mux.HandleFunc("GET /clusters", handlers.ClustersByCloud(s.tmpls, s.metaRegion, s.region))
	// HTMX partial: background refresh (10s poll) — returns inner content only, no OOB tab bar
	s.mux.HandleFunc("GET /clusters/data", handlers.ClusterDataRefresh(s.tmpls, s.metaRegion, s.region))
	s.mux.HandleFunc("GET /about", handlers.About(s.tmpls))

	// Cluster detail pages and section HTMX fragments
	s.mux.HandleFunc("GET /clusters/{name}", handlers.ClusterDetail(s.tmpls, s.metaRegion, s.region))
	s.mux.HandleFunc("GET /clusters/{name}/sections/{section}",
		handlers.ClusterSection(s.tmpls, s.metaRegion, s.region))
	s.mux.HandleFunc("POST /clusters/{name}/stages/run",
		handlers.ClusterStagesRun(s.tmpls, s.metaRegion, s.region, s.broker, s.localMode, s.repoRoot))

	// Install wizard and SSE progress stream
	s.mux.HandleFunc("GET /install", handlers.InstallWizard(s.tmpls))
	s.mux.HandleFunc("POST /install/step2", handlers.InstallStep2(s.tmpls))
	s.mux.HandleFunc("POST /install/step3", handlers.InstallStep3(s.tmpls))
	s.mux.HandleFunc("POST /install/step4", handlers.InstallStep4(s.tmpls))
	s.mux.HandleFunc("POST /install/preview", handlers.InstallPreview(s.tmpls))
	s.mux.HandleFunc("POST /install/confirm", handlers.InstallConfirm(s.tmpls, s.metaRegion, s.broker, s.localMode, s.repoRoot))
	s.mux.HandleFunc("GET /install/stream/{sessionID}", handlers.InstallStream(s.tmpls, s.broker))
	s.mux.HandleFunc("GET /install/retry/{sessionID}", handlers.InstallRetry(s.tmpls, s.broker))
	s.mux.HandleFunc("GET /install/retry/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/install", http.StatusSeeOther)
	})
	s.mux.HandleFunc("GET /install/retry", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/install", http.StatusSeeOther)
	})
	s.mux.HandleFunc("GET /install/sse/{sessionID}", handlers.InstallSSE(s.broker))

	// Chart-only mode and values editor
	s.mux.HandleFunc("GET /chart-only", handlers.ChartOnlyLanding(s.tmpls))
	s.mux.HandleFunc("POST /chart-only/validate", handlers.ChartOnlyValidate(s.tmpls))
	s.mux.HandleFunc("POST /chart-only/configure", handlers.ChartOnlyConfigure(s.tmpls, s.schema))
	s.mux.HandleFunc("POST /chart-only/render", handlers.ChartOnlyRender(s.tmpls, s.schema, s.chartDir))
	s.mux.HandleFunc("POST /chart-only/preview", handlers.ChartOnlyPreview(s.tmpls, s.schema))
	s.mux.HandleFunc("POST /chart-only/apply", handlers.ChartOnlyApply(s.tmpls, s.schema, s.broker, s.chartDir))
	s.mux.HandleFunc("GET /chart-only/stream/{sessionID}", handlers.ChartOnlyStream(s.tmpls, s.broker))
	s.mux.HandleFunc("GET /chart-only/sse/{sessionID}", handlers.ChartOnlySSE(s.broker))

	// Destroy flow
	s.mux.HandleFunc("GET /clusters/{name}/destroy", handlers.DestroyConfirm(s.tmpls, s.metaRegion, s.region))
	s.mux.HandleFunc("POST /clusters/{name}/destroy", handlers.DestroyApply(s.tmpls, s.metaRegion, s.region, s.broker, s.localMode, s.repoRoot))
	s.mux.HandleFunc("GET /clusters/{name}/destroy/stream/{sessionID}", handlers.DestroyStream(s.tmpls, s.broker))
	s.mux.HandleFunc("GET /clusters/{name}/destroy/sse/{sessionID}", handlers.DestroySSE(s.broker))

	// Operations (doctor, versions, support bundle, history)
	s.mux.HandleFunc("GET /operations", handlers.Operations(s.tmpls, s.metaRegion, s.region))
	// HTMX partial: tab switch on operations overview
	s.mux.HandleFunc("GET /operations/clusters", handlers.OperationsByCloud(s.tmpls, s.metaRegion, s.region))
	// HTMX partial: background refresh for operations overview
	s.mux.HandleFunc("GET /operations/data", handlers.OpsDataRefresh(s.tmpls, s.metaRegion, s.region))
	s.mux.HandleFunc("GET /clusters/{name}/operations", handlers.OpsDoctor(s.tmpls, s.metaRegion, s.region))
	s.mux.HandleFunc("POST /clusters/{name}/operations/doctor/run",
		handlers.OpsDoctorRun(s.tmpls, s.metaRegion, s.region, s.broker))
	s.mux.HandleFunc("GET /clusters/{name}/operations/doctor/stream/{sessionID}",
		handlers.OpsDoctorStream(s.tmpls, s.broker))
	s.mux.HandleFunc("GET /clusters/{name}/operations/doctor/sse/{sessionID}",
		handlers.OpsDoctorSSE(s.broker))
	s.mux.HandleFunc("GET /clusters/{name}/operations/versions",
		handlers.OpsVersions(s.tmpls))
	s.mux.HandleFunc("GET /clusters/{name}/operations/bundle",
		handlers.OpsBundle(s.tmpls))
	s.mux.HandleFunc("POST /clusters/{name}/operations/bundle",
		handlers.OpsBundleRun(s.tmpls, s.metaRegion, s.region, s.broker, s.bundleStore))
	s.mux.HandleFunc("GET /clusters/{name}/operations/bundle/stream/{sessionID}",
		handlers.OpsBundleStream(s.tmpls, s.broker))
	s.mux.HandleFunc("GET /clusters/{name}/operations/bundle/sse/{sessionID}",
		handlers.OpsBundleSSE(s.broker))
	s.mux.HandleFunc("GET /clusters/{name}/operations/bundle/download/{token}",
		handlers.OpsBundleDownload(s.bundleStore))
	s.mux.HandleFunc("GET /clusters/{name}/operations/history",
		handlers.OpsHistory(s.tmpls, s.metaRegion, s.region))

	// Health — no auth, no template
	s.mux.HandleFunc("GET /healthz", handlers.Healthz())
}

// parseTemplates parses layout.html as the base and creates per-page clones
// so each page's {{define "content"}} block does not conflict with others.
//
// localMode and repoRoot drive the devMode / devModeRepo template functions
// used by layout.html to render the development banner.
func parseTemplates(localMode bool, repoRoot string) (*handlers.Templates, error) {
	devFuncs := template.FuncMap{
		"devMode":     func() bool { return localMode },
		"devModeRepo": func() string { return repoRoot },
	}
	base, err := template.New("base").Funcs(handlers.FuncMap()).Funcs(devFuncs).ParseFS(FS,
		"templates/layout.html",
		"templates/_help_overlay.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse layout: %w", err)
	}

	home, err := template.Must(base.Clone()).ParseFS(FS, "templates/home.html")
	if err != nil {
		return nil, fmt.Errorf("parse home: %w", err)
	}

	about, err := template.Must(base.Clone()).ParseFS(FS, "templates/about.html")
	if err != nil {
		return nil, fmt.Errorf("parse about: %w", err)
	}

	cluster, err := template.Must(base.Clone()).ParseFS(FS,
		"templates/cluster.html",
		"templates/_cluster_overview.html",
		"templates/_cluster_workloads.html",
		"templates/_cluster_kafka.html",
		"templates/_cluster_storage.html",
		"templates/_cluster_addons.html",
		"templates/_cluster_stages.html",
		"templates/_cluster_activity.html",
		"templates/_cluster_config.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse cluster: %w", err)
	}

	// Install wizard: all 4 step fragments + hidden-input fragments live in
	// the same template set so hx-target="#wizard-body" can swap any of them.
	installWizard, err := template.Must(base.Clone()).ParseFS(FS, "templates/install.html")
	if err != nil {
		return nil, fmt.Errorf("parse install wizard: %w", err)
	}

	installPreview, err := template.Must(base.Clone()).ParseFS(FS, "templates/install_preview.html")
	if err != nil {
		return nil, fmt.Errorf("parse install preview: %w", err)
	}

	installStream, err := template.Must(base.Clone()).ParseFS(FS, "templates/install_stream.html")
	if err != nil {
		return nil, fmt.Errorf("parse install stream: %w", err)
	}

	// Chart-only templates (landing includes the validate-result fragment).
	chartOnlyLanding, err := template.Must(base.Clone()).ParseFS(FS, "templates/chart_only_landing.html")
	if err != nil {
		return nil, fmt.Errorf("parse chart only landing: %w", err)
	}

	// Values editor includes the live preview fragment.
	chartValuesEditor, err := template.Must(base.Clone()).ParseFS(FS, "templates/chart_values_editor.html")
	if err != nil {
		return nil, fmt.Errorf("parse chart values editor: %w", err)
	}

	chartPreview, err := template.Must(base.Clone()).ParseFS(FS, "templates/chart_preview.html")
	if err != nil {
		return nil, fmt.Errorf("parse chart preview: %w", err)
	}

	chartApply, err := template.Must(base.Clone()).ParseFS(FS, "templates/chart_apply.html")
	if err != nil {
		return nil, fmt.Errorf("parse chart apply: %w", err)
	}

	destroyConfirm, err := template.Must(base.Clone()).ParseFS(FS, "templates/destroy_confirm.html")
	if err != nil {
		return nil, fmt.Errorf("parse destroy confirm: %w", err)
	}

	destroyStream, err := template.Must(base.Clone()).ParseFS(FS, "templates/destroy_stream.html")
	if err != nil {
		return nil, fmt.Errorf("parse destroy stream: %w", err)
	}

	// Operations pages share _operations_nav.html.
	opsBase := func() *template.Template {
		return template.Must(base.Clone())
	}

	opsOverview, err := opsBase().ParseFS(FS, "templates/operations_overview.html")
	if err != nil {
		return nil, fmt.Errorf("parse ops overview: %w", err)
	}

	opsDoctor, err := opsBase().ParseFS(FS,
		"templates/operations_doctor.html",
		"templates/_operations_nav.html",
		"templates/_finding.html",
		"templates/_finding_group.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse ops doctor: %w", err)
	}

	opsDoctorRunning, err := opsBase().ParseFS(FS, "templates/operations_doctor_running.html")
	if err != nil {
		return nil, fmt.Errorf("parse ops doctor running: %w", err)
	}

	opsBundle, err := opsBase().ParseFS(FS,
		"templates/operations_bundle.html",
		"templates/_operations_nav.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse ops bundle: %w", err)
	}

	opsVersions, err := opsBase().ParseFS(FS,
		"templates/operations_versions.html",
		"templates/_operations_nav.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse ops versions: %w", err)
	}

	opsHistory, err := opsBase().ParseFS(FS,
		"templates/operations_history.html",
		"templates/_operations_nav.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse ops history: %w", err)
	}

	return &handlers.Templates{
		Home:           home,
		About:          about,
		Cluster:        cluster,
		InstallWizard:  installWizard,
		InstallPreview: installPreview,
		InstallStream:  installStream,
		ChartOnlyLanding:  chartOnlyLanding,
		ChartValuesEditor: chartValuesEditor,
		ChartPreview:      chartPreview,
		ChartApply:        chartApply,
		DestroyConfirm: destroyConfirm,
		DestroyStream:  destroyStream,
		OpsOverview:      opsOverview,
		OpsDoctor:        opsDoctor,
		OpsDoctorRunning: opsDoctorRunning,
		OpsBundle:        opsBundle,
		OpsVersions:      opsVersions,
		OpsHistory:       opsHistory,
	}, nil
}
