package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kaz-under-the-bridge/google-cloud-ops-mcp/internal/logging"
	"github.com/kaz-under-the-bridge/google-cloud-ops-mcp/internal/mcp"
	"github.com/kaz-under-the-bridge/google-cloud-ops-mcp/internal/monitoring"
)

const (
	serverName    = "gcp-ops-mcp"
	serverVersion = "0.1.0"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	// Create MCP server
	server := mcp.NewServer(serverName, serverVersion)

	// Create Cloud Logging client
	loggingClient, err := logging.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create logging client: %w", err)
	}
	defer loggingClient.Close()

	// Create Cloud Monitoring client
	monitoringClient, err := monitoring.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create monitoring client: %w", err)
	}
	defer monitoringClient.Close()

	// Register logging.query tool
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
					Description: "Maximum number of entries to return (default: 200, max: 500)",
					Default:     200,
				},
			},
			Required: []string{"project_id"},
		},
	}, loggingClient.QueryHandler())

	// Register monitoring.query_time_series tool
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
					Description: "Maximum number of time series to return (default: 20, max: 50)",
					Default:     20,
				},
			},
			Required: []string{"project_id", "metric_type"},
		},
	}, monitoringClient.QueryTimeSeriesHandler())

	// Run server
	return server.Run(ctx)
}
