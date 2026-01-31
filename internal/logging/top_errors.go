package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"cloud.google.com/go/logging/apiv2/loggingpb"
	"google.golang.org/api/iterator"
)

// TopErrorsParams are the parameters for logging.top_errors
type TopErrorsParams struct {
	ProjectID string    `json:"project_id"`
	TimeRange TimeRange `json:"time_range"`
	GroupBy   string    `json:"group_by"` // "log_name", "message", "resource_type"
	Limit     int       `json:"limit"`    // Top N errors to return
}

// TopErrorsResult is the result of logging.top_errors
type TopErrorsResult struct {
	QueryMeta   TopErrorsQueryMeta `json:"query_meta"`
	ErrorGroups []ErrorGroup       `json:"error_groups"`
	Stats       TopErrorsStats     `json:"stats"`
}

type TopErrorsQueryMeta struct {
	ProjectID string `json:"project_id"`
	Start     string `json:"start"`
	End       string `json:"end"`
	GroupBy   string `json:"group_by"`
}

type ErrorGroup struct {
	Key         string    `json:"key"`
	Count       int       `json:"count"`
	Percentage  float64   `json:"percentage"`
	FirstSeen   string    `json:"first_seen"`
	LastSeen    string    `json:"last_seen"`
	SampleEntry *LogEntry `json:"sample_entry,omitempty"`
}

type TopErrorsStats struct {
	TotalErrors  int `json:"total_errors"`
	UniqueGroups int `json:"unique_groups"`
	ScannedLogs  int `json:"scanned_logs"`
}

// TopErrors aggregates error logs and returns top N
func (c *Client) TopErrors(ctx context.Context, params TopErrorsParams) (*TopErrorsResult, error) {
	// Parse time range
	startTime, endTime, err := parseTimeRange(params.TimeRange)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time range: %w", err)
	}

	// Set defaults
	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	groupBy := params.GroupBy
	if groupBy == "" {
		groupBy = "log_name"
	}

	// Build filter for ERROR and above
	filter := fmt.Sprintf(`severity >= ERROR AND timestamp >= "%s" AND timestamp <= "%s"`,
		startTime.Format(time.RFC3339),
		endTime.Format(time.RFC3339))

	// Create request - fetch more entries to get good aggregation
	req := &loggingpb.ListLogEntriesRequest{
		ResourceNames: []string{fmt.Sprintf("projects/%s", params.ProjectID)},
		Filter:        filter,
		OrderBy:       "timestamp desc",
		PageSize:      1000, // Scan up to 1000 entries for aggregation
	}

	// Execute query and aggregate
	it := c.client.ListLogEntries(ctx, req)

	groups := make(map[string]*errorGroupBuilder)
	scannedCount := 0
	maxScan := 1000 // Limit scanning for performance

	for scannedCount < maxScan {
		entry, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate log entries: %w", err)
		}

		scannedCount++
		logEntry := convertLogEntry(entry)
		key := getGroupKey(logEntry, groupBy)

		if group, exists := groups[key]; exists {
			group.count++
			if logEntry.Timestamp < group.firstSeen {
				group.firstSeen = logEntry.Timestamp
			}
			if logEntry.Timestamp > group.lastSeen {
				group.lastSeen = logEntry.Timestamp
			}
		} else {
			groups[key] = &errorGroupBuilder{
				key:         key,
				count:       1,
				firstSeen:   logEntry.Timestamp,
				lastSeen:    logEntry.Timestamp,
				sampleEntry: &logEntry,
			}
		}
	}

	// Convert to sorted slice
	totalErrors := 0
	var groupList []*errorGroupBuilder
	for _, g := range groups {
		totalErrors += g.count
		groupList = append(groupList, g)
	}

	// Sort by count descending
	sort.Slice(groupList, func(i, j int) bool {
		return groupList[i].count > groupList[j].count
	})

	// Take top N
	if len(groupList) > limit {
		groupList = groupList[:limit]
	}

	// Build result
	errorGroups := make([]ErrorGroup, len(groupList))
	for i, g := range groupList {
		percentage := 0.0
		if totalErrors > 0 {
			percentage = float64(g.count) / float64(totalErrors) * 100
		}
		errorGroups[i] = ErrorGroup{
			Key:         g.key,
			Count:       g.count,
			Percentage:  percentage,
			FirstSeen:   g.firstSeen,
			LastSeen:    g.lastSeen,
			SampleEntry: g.sampleEntry,
		}
	}

	return &TopErrorsResult{
		QueryMeta: TopErrorsQueryMeta{
			ProjectID: params.ProjectID,
			Start:     startTime.Format(time.RFC3339),
			End:       endTime.Format(time.RFC3339),
			GroupBy:   groupBy,
		},
		ErrorGroups: errorGroups,
		Stats: TopErrorsStats{
			TotalErrors:  totalErrors,
			UniqueGroups: len(groups),
			ScannedLogs:  scannedCount,
		},
	}, nil
}

type errorGroupBuilder struct {
	key         string
	count       int
	firstSeen   string
	lastSeen    string
	sampleEntry *LogEntry
}

func getGroupKey(entry LogEntry, groupBy string) string {
	switch groupBy {
	case "log_name":
		return entry.LogName
	case "resource_type":
		return entry.Resource.Type
	case "message":
		// Use first 100 chars of payload as key
		msg := entry.TextPayload
		if msg == "" && entry.JSONPayload != nil {
			if m, ok := entry.JSONPayload["message"].(string); ok {
				msg = m
			}
		}
		if len(msg) > 100 {
			msg = msg[:100]
		}
		return msg
	default:
		return entry.LogName
	}
}

// TopErrorsHandler returns a handler for the logging.top_errors tool
func (c *Client) TopErrorsHandler() func(ctx context.Context, args json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var params TopErrorsParams
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("failed to parse arguments: %w", err)
		}

		if params.ProjectID == "" {
			return nil, fmt.Errorf("project_id is required")
		}

		return c.TopErrors(ctx, params)
	}
}

// TopErrorsHandlerWithGuardrail returns a handler with guardrail validation
func (c *Client) TopErrorsHandlerWithGuardrail(v Validator) func(ctx context.Context, args json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var params TopErrorsParams
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("failed to parse arguments: %w", err)
		}

		if params.ProjectID == "" {
			return nil, fmt.Errorf("project_id is required")
		}

		// ガードレール: プロジェクトID検証
		if err := v.ValidateProjectID(params.ProjectID); err != nil {
			return nil, err
		}

		// 時間範囲のパース
		startTime, endTime, err := parseTimeRange(params.TimeRange)
		if err != nil {
			return nil, fmt.Errorf("failed to parse time range: %w", err)
		}

		// ガードレール: 時間範囲検証
		if err := v.ValidateTimeRange(startTime, endTime); err != nil {
			return nil, err
		}

		return c.TopErrors(ctx, params)
	}
}
