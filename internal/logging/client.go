package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cloud.google.com/go/logging/apiv2/loggingpb"
	logging "cloud.google.com/go/logging/apiv2"
	"google.golang.org/api/iterator"
)

// QueryParams are the parameters for logging.query
type QueryParams struct {
	ProjectID string    `json:"project_id"`
	Filter    string    `json:"filter"`
	TimeRange TimeRange `json:"time_range"`
	Limit     int       `json:"limit"`
}

type TimeRange struct {
	Start string `json:"start"` // RFC3339 or relative ("-1h", "-30m")
	End   string `json:"end"`   // RFC3339 or "now"
}

// QueryResult is the result of logging.query
type QueryResult struct {
	QueryMeta QueryMeta    `json:"query_meta"`
	Entries   []LogEntry   `json:"entries"`
	Stats     ResultStats  `json:"stats"`
}

type QueryMeta struct {
	ProjectID string `json:"project_id"`
	Start     string `json:"start"`
	End       string `json:"end"`
	Filter    string `json:"filter"`
	Limit     int    `json:"limit"`
}

type LogEntry struct {
	Timestamp   string         `json:"timestamp"`
	Severity    string         `json:"severity"`
	LogName     string         `json:"log_name"`
	Resource    Resource       `json:"resource"`
	Labels      map[string]string `json:"labels,omitempty"`
	Trace       string         `json:"trace,omitempty"`
	SpanID      string         `json:"span_id,omitempty"`
	TextPayload string         `json:"text_payload,omitempty"`
	JSONPayload map[string]any `json:"json_payload,omitempty"`
	InsertID    string         `json:"insert_id"`
}

type Resource struct {
	Type   string            `json:"type"`
	Labels map[string]string `json:"labels,omitempty"`
}

type ResultStats struct {
	ReturnedCount int  `json:"returned_count"`
	Sampled       bool `json:"sampled"`
}

// Client is the Cloud Logging client
type Client struct {
	client *logging.Client
}

// NewClient creates a new Cloud Logging client
func NewClient(ctx context.Context) (*Client, error) {
	client, err := logging.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create logging client: %w", err)
	}
	return &Client{client: client}, nil
}

// Close closes the client
func (c *Client) Close() error {
	return c.client.Close()
}

// Query executes a log query
func (c *Client) Query(ctx context.Context, params QueryParams) (*QueryResult, error) {
	// Parse time range
	startTime, endTime, err := parseTimeRange(params.TimeRange)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time range: %w", err)
	}

	// Set default limit
	limit := params.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}

	// Build filter with time range
	filter := params.Filter
	if filter != "" {
		filter += " AND "
	}
	filter += fmt.Sprintf(`timestamp >= "%s" AND timestamp <= "%s"`,
		startTime.Format(time.RFC3339),
		endTime.Format(time.RFC3339))

	// Create request
	req := &loggingpb.ListLogEntriesRequest{
		ResourceNames: []string{fmt.Sprintf("projects/%s", params.ProjectID)},
		Filter:        filter,
		OrderBy:       "timestamp desc",
		PageSize:      int32(limit),
	}

	// Execute query
	it := c.client.ListLogEntries(ctx, req)

	entries := []LogEntry{}
	for {
		entry, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate log entries: %w", err)
		}

		logEntry := convertLogEntry(entry)
		entries = append(entries, logEntry)

		if len(entries) >= limit {
			break
		}
	}

	return &QueryResult{
		QueryMeta: QueryMeta{
			ProjectID: params.ProjectID,
			Start:     startTime.Format(time.RFC3339),
			End:       endTime.Format(time.RFC3339),
			Filter:    params.Filter,
			Limit:     limit,
		},
		Entries: entries,
		Stats: ResultStats{
			ReturnedCount: len(entries),
			Sampled:       false,
		},
	}, nil
}

func parseTimeRange(tr TimeRange) (time.Time, time.Time, error) {
	now := time.Now()
	var startTime, endTime time.Time
	var err error

	// Parse end time
	if tr.End == "" || tr.End == "now" {
		endTime = now
	} else {
		endTime, err = time.Parse(time.RFC3339, tr.End)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end time: %w", err)
		}
	}

	// Parse start time
	if tr.Start == "" {
		startTime = now.Add(-30 * time.Minute) // default: 30 minutes ago
	} else if len(tr.Start) > 0 && tr.Start[0] == '-' {
		// Relative time (e.g., "-1h", "-30m")
		duration, err := time.ParseDuration(tr.Start[1:])
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid relative start time: %w", err)
		}
		startTime = now.Add(-duration)
	} else {
		startTime, err = time.Parse(time.RFC3339, tr.Start)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start time: %w", err)
		}
	}

	return startTime, endTime, nil
}

func convertLogEntry(entry *loggingpb.LogEntry) LogEntry {
	le := LogEntry{
		Timestamp: entry.GetTimestamp().AsTime().Format(time.RFC3339),
		Severity:  entry.GetSeverity().String(),
		LogName:   entry.GetLogName(),
		InsertID:  entry.GetInsertId(),
		Trace:     entry.GetTrace(),
		SpanID:    entry.GetSpanId(),
		Labels:    entry.GetLabels(),
	}

	// Resource
	if res := entry.GetResource(); res != nil {
		le.Resource = Resource{
			Type:   res.GetType(),
			Labels: res.GetLabels(),
		}
	}

	// Payload
	switch p := entry.GetPayload().(type) {
	case *loggingpb.LogEntry_TextPayload:
		le.TextPayload = p.TextPayload
	case *loggingpb.LogEntry_JsonPayload:
		if p.JsonPayload != nil {
			le.JSONPayload = structToMap(p.JsonPayload)
		}
	}

	return le
}

func structToMap(s interface{ AsMap() map[string]any }) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

// QueryHandler returns a handler for the logging.query tool
func (c *Client) QueryHandler() func(ctx context.Context, args json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var params QueryParams
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("failed to parse arguments: %w", err)
		}

		if params.ProjectID == "" {
			return nil, fmt.Errorf("project_id is required")
		}

		return c.Query(ctx, params)
	}
}
