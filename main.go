package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kaz-under-the-bridge/google-cloud-ops-mcp/internal/config"
	"github.com/kaz-under-the-bridge/google-cloud-ops-mcp/internal/guardrail"
	"github.com/kaz-under-the-bridge/google-cloud-ops-mcp/internal/logging"
	"github.com/kaz-under-the-bridge/google-cloud-ops-mcp/internal/mcp"
	"github.com/kaz-under-the-bridge/google-cloud-ops-mcp/internal/monitoring"
)

const (
	serverName    = "gcp-ops-mcp"
	serverVersion = "0.3.0"
)

func main() {
	os.Exit(realMain())
}

func realMain() int {
	// Parse flags
	configPath := flag.String("config", "", "Path to config file (optional)")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := run(ctx, *configPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, configPath string) error {
	// Load config
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create guardrail
	guard := guardrail.New(cfg)

	// Create MCP server
	server := mcp.NewServer(serverName, serverVersion)

	// Create Cloud Logging client
	loggingClient, err := logging.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create logging client: %w", err)
	}
	defer func() { _ = loggingClient.Close() }()

	// Create Cloud Monitoring client
	monitoringClient, err := monitoring.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create monitoring client: %w", err)
	}
	defer func() { _ = monitoringClient.Close() }()

	// Register logging.query tool (with guardrail)
	server.RegisterTool(mcp.Tool{
		Name:        "logging.query",
		Description: "Search Cloud Logging logs. Equivalent to Logs Explorer.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"project_id": {
					Type:        "string",
					Description: "GCP project ID",
				},
				"filter": {
					Type:        "string",
					Description: "Logging Query Language filter (e.g., 'severity>=ERROR')",
				},
				"time_range": {
					Type:        "object",
					Description: "Time range for the query",
					Properties: map[string]mcp.Property{
						"start": {
							Type:        "string",
							Description: "Start time (RFC3339 or relative like '-1h', '-30m')",
						},
						"end": {
							Type:        "string",
							Description: "End time (RFC3339 or 'now')",
							Default:     "now",
						},
					},
				},
				"limit": {
					Type:        "integer",
					Description: fmt.Sprintf("Maximum number of entries to return (default: 200, max: %d)", cfg.Limits.MaxLogEntries),
					Default:     200,
				},
			},
			Required: []string{"project_id"},
		},
	}, loggingClient.QueryHandlerWithGuardrail(guard))

	// Register monitoring.query_time_series tool (with guardrail)
	server.RegisterTool(mcp.Tool{
		Name:        "monitoring.query_time_series",
		Description: "Query Cloud Monitoring time series data.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"project_id": {
					Type:        "string",
					Description: "GCP project ID",
				},
				"metric_type": {
					Type:        "string",
					Description: "Metric type (e.g., 'run.googleapis.com/request_count')",
				},
				"resource_type": {
					Type:        "string",
					Description: "Resource type (e.g., 'cloud_run_revision')",
				},
				"filters": {
					Type:        "object",
					Description: "Additional filters as key-value pairs",
				},
				"alignment_period_sec": {
					Type:        "integer",
					Description: "Alignment period in seconds (default: 60)",
					Default:     60,
				},
				"time_range": {
					Type:        "object",
					Description: "Time range for the query",
					Properties: map[string]mcp.Property{
						"start": {
							Type:        "string",
							Description: "Start time (RFC3339 or relative like '-1h', '-30m')",
						},
						"end": {
							Type:        "string",
							Description: "End time (RFC3339 or 'now')",
							Default:     "now",
						},
					},
				},
				"max_series": {
					Type:        "integer",
					Description: fmt.Sprintf("Maximum number of time series to return (default: 20, max: %d)", cfg.Limits.MaxTimeSeries),
					Default:     20,
				},
			},
			Required: []string{"project_id", "metric_type"},
		},
	}, monitoringClient.QueryTimeSeriesHandlerWithGuardrail(guard))

	// Register logging.top_errors tool (with guardrail)
	server.RegisterTool(mcp.Tool{
		Name:        "logging.top_errors",
		Description: "Aggregate error logs and return top N most frequent errors. Useful for identifying common issues.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"project_id": {
					Type:        "string",
					Description: "GCP project ID",
				},
				"time_range": {
					Type:        "object",
					Description: "Time range for the query",
					Properties: map[string]mcp.Property{
						"start": {
							Type:        "string",
							Description: "Start time (RFC3339 or relative like '-1h', '-30m')",
						},
						"end": {
							Type:        "string",
							Description: "End time (RFC3339 or 'now')",
							Default:     "now",
						},
					},
				},
				"group_by": {
					Type:        "string",
					Description: "How to group errors: 'log_name', 'resource_type', or 'message' (default: 'log_name')",
					Default:     "log_name",
				},
				"limit": {
					Type:        "integer",
					Description: "Number of top error groups to return (default: 10, max: 50)",
					Default:     10,
				},
			},
			Required: []string{"project_id"},
		},
	}, loggingClient.TopErrorsHandlerWithGuardrail(guard))

	// Register monitoring.list_metric_descriptors tool (with guardrail)
	server.RegisterTool(mcp.Tool{
		Name:        "monitoring.list_metric_descriptors",
		Description: "List available metric descriptors in a project. Useful for discovering what metrics are available.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"project_id": {
					Type:        "string",
					Description: "GCP project ID",
				},
				"filter": {
					Type:        "string",
					Description: "Optional filter (e.g., 'metric.type = starts_with(\"run.googleapis.com\")')",
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum number of descriptors to return (default: 100, max: 500)",
					Default:     100,
				},
			},
			Required: []string{"project_id"},
		},
	}, monitoringClient.ListMetricDescriptorsHandlerWithGuardrail(guard))

	// Run server
	return server.Run(ctx)
}
