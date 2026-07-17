package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/WolframResearch/WAS-Kubernetes/internal/webui"
)

var (
	flagServePort      string
	flagServeBind      string
	flagServeNoBrowser bool
	flagServeChartDir  string
)

var serveCmd = &cobra.Command{
	Use:          "serve",
	Short:        "Launch the wasctl web UI",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "wasctl: config not loaded (%v) — using defaults\n", err)
		}

		metaRegion := "us-east-1"
		region := "us-east-1"
		if cfg != nil {
			if cfg.MetaRegion.Value != "" {
				metaRegion = cfg.MetaRegion.Value
			}
			if cfg.Region.Value != "" {
				region = cfg.Region.Value
			}
		}

		addr := net.JoinHostPort(flagServeBind, flagServePort)

		// Warn loudly when binding beyond localhost.
		if flagServeBind != "127.0.0.1" && flagServeBind != "localhost" && flagServeBind != "::1" {
			fmt.Fprintf(os.Stderr,
				"\nWARNING: Binding to %s exposes the wasctl web UI to the network.\n"+
					"         The UI has no authentication and can destroy clusters.\n"+
					"         Use only on isolated, trusted networks.\n\n",
				flagServeBind)
		}

		localRoot := ""
		if flagLocal {
			var err2 error
			localRoot, err2 = filepath.Abs(".")
			if err2 != nil {
				localRoot = "."
			}
		}

		srv, err := webui.NewServer(addr, metaRegion, region, flagServeChartDir, flagLocal, localRoot)
		if err != nil {
			return fmt.Errorf("create server: %w", err)
		}

		url := "http://" + addr
		fmt.Printf("wasctl UI  →  %s\n", url)

		if !flagServeNoBrowser {
			go tryOpenBrowser(url)
		}

		// Graceful shutdown on SIGINT / SIGTERM.
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

		httpSrv := &http.Server{
			Addr:    addr,
			Handler: srv.Handler(),
		}

		errCh := make(chan error, 1)
		go func() { errCh <- httpSrv.ListenAndServe() }()

		select {
		case err := <-errCh:
			return err
		case <-stop:
			fmt.Println("\nShutting down...")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return httpSrv.Shutdown(ctx)
		}
	},
}

func init() {
	serveCmd.Flags().StringVarP(&flagServePort, "port", "p", "8765",
		"port to listen on")
	serveCmd.Flags().StringVar(&flagServeBind, "bind", "127.0.0.1",
		"address to bind to (use 0.0.0.0 to allow remote access — see warning)")
	serveCmd.Flags().BoolVar(&flagServeNoBrowser, "no-browser", false,
		"do not auto-open the default browser on start")
	serveCmd.Flags().StringVar(&flagServeChartDir, "chart-dir",
		"charts/wolfram-application-server",
		"path to the WAS Helm chart directory (used for chart-only install mode)")
}

// tryOpenBrowser attempts to open url in the default browser. Runs after a
// short delay to let the server start. Failures are silently ignored.
func tryOpenBrowser(url string) {
	time.Sleep(300 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}
