package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"

	"github.com/SammyLin/openclaw-exporter/collector"
)

const version = "0.3.0"

func main() {
	var (
		listenAddress = kingpin.Flag("web.listen-address", "Address to listen on.").
				Default(":9101").Envar("EXPORTER_LISTEN_ADDRESS").String()
		telemetryPath = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").
				Default("/metrics").Envar("EXPORTER_TELEMETRY_PATH").String()
		openclawHome = kingpin.Flag("openclaw.home", "Path to OpenClaw home directory.").
				Default("~/.openclaw").Envar("OPENCLAW_HOME").String()
		logLevel = kingpin.Flag("log.level", "Log level (debug, info, warn, error).").
				Default("info").Envar("EXPORTER_LOG_LEVEL").String()
	)

	kingpin.Version(version)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	// Configure slog
	var level slog.Level
	switch strings.ToLower(*logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	// Expand ~ in home path
	home := *openclawHome
	if strings.HasPrefix(home, "~/") {
		userHome, err := os.UserHomeDir()
		if err != nil {
			slog.Error("Failed to get user home directory", "err", err)
			os.Exit(1)
		}
		home = userHome + home[1:]
	}

	slog.Info("Starting openclaw-exporter", "version", version, "listen", *listenAddress, "home", home)

	// Create custom registry
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collector.NewAgentCollector(home),
		collector.NewCronCollector(home),
		collector.NewWorkspaceCollector(home),
		collector.NewTokenCollector(home),
	)

	// Set up HTTP
	mux := http.NewServeMux()
	mux.Handle(*telemetryPath, promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><head><title>OpenClaw Exporter</title></head>
<body><h1>OpenClaw Exporter</h1><p><a href="%s">Metrics</a></p></body></html>`, *telemetryPath)
	})

	server := &http.Server{Handler: mux}

	if err := web.ListenAndServe(server, &web.FlagConfig{
		WebListenAddresses: &[]string{*listenAddress},
		WebConfigFile:      new(string),
	}, slog.Default()); err != nil {
		slog.Error("Failed to start server", "err", err)
		os.Exit(1)
	}
}
