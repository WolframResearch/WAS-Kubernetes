package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

type historyRow struct {
	ID          string
	GeneratedAt string
	Age         string
	Pass        int
	Fail        int
	Skip        int
	StatusClass string
}

type opsHistoryData struct {
	Version     string
	ClusterName string
	ActiveTab   string
	Rows        []historyRow
	Error       string
}

// OpsHistory handles GET /clusters/{name}/operations/history.
func OpsHistory(t *Templates, metaRegion, region string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		data := opsHistoryData{
			Version:     version.Version,
			ClusterName: name,
			ActiveTab:   "history",
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		rows, errMsg := loadHistoryRows(ctx, metaRegion, name, preferredCloud(r))
		data.Rows = rows
		data.Error = errMsg

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.OpsHistory.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

func loadHistoryRows(ctx context.Context, metaRegion, clusterName, preferCloud string) ([]historyRow, string) {
	store, _, err := openReportStore(ctx, metaRegion, clusterName, preferCloud)
	if err != nil {
		return nil, err.Error()
	}

	entries, err := workspace.ListReportEntries(ctx, store, clusterName)
	if err != nil {
		return nil, "could not load report list: " + err.Error()
	}

	rows := make([]historyRow, 0, len(entries))
	for _, e := range entries {
		statusClass, _ := opsStatusClass(e.Summary.Fail)
		rows = append(rows, historyRow{
			ID:          e.ID,
			GeneratedAt: e.GeneratedAt.UTC().Format("2006-01-02 15:04 UTC"),
			Age:         formatAge(e.GeneratedAt),
			Pass:        e.Summary.Pass,
			Fail:        e.Summary.Fail,
			Skip:        e.Summary.Skip,
			StatusClass: statusClass,
		})
	}
	return rows, ""
}
