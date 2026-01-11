package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// QueryTimeSeriesParams are the parameters for monitoring.query_time_series
type QueryTimeSeriesParams struct {
	ProjectID          string            `json:"project_id"`
	MetricType         string            `json:"metric_type"`
	ResourceType       string            `json:"resource_type,omitempty"`
	Filters            map[string]string `json:"filters,omitempty"`
	AlignmentPeriodSec int               `json:"alignment_period_sec"`
	TimeRange          TimeRange         `json:"time_range"`
	MaxSeries          int               `json:"max_series"`
}

type TimeRange struct {
	Start string `json:"start"` // RFC3339 or relative ("-1h", "-30m")
	End   string `json:"end"`   // RFC3339 or "now"
}

// QueryTimeSeriesResult is the result of monitoring.query_time_series
type QueryTimeSeriesResult struct {
	QueryMeta QueryMeta    `json:"query_meta"`
	Series    []TimeSeries `json:"series"`
	Stats     ResultStats  `json:"stats"`
}

type QueryMeta struct {
	ProjectID  string `json:"project_id"`
	MetricType string `json:"metric_type"`
	Start      string `json:"start"`
	End        string `json:"end"`
}

type TimeSeries struct {
	Metric   MetricLabels   `json:"metric"`
	Resource ResourceLabels `json:"resource"`
	Points   []DataPoint    `json:"points"`
}

type MetricLabels struct {
	Type   string            `json:"type"`
	Labels map[string]string `json:"labels,omitempty"`
}

type ResourceLabels struct {
	Type   string            `json:"type"`
	Labels map[string]string `json:"labels,omitempty"`
}

type DataPoint struct {
	Time  string  `json:"time"`
	Value float64 `json:"value"`
}

type ResultStats struct {
	SeriesCount     int `json:"series_count"`
	PointCountTotal int `json:"point_count_total"`
}

// Client is the Cloud Monitoring client
type Client struct {
	metricClient *monitoring.MetricClient
}

// NewClient creates a new Cloud Monitoring client
func NewClient(ctx context.Context) (*Client, error) {
	metricClient, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create monitoring client: %w", err)
	}
	return &Client{metricClient: metricClient}, nil
}

// Close closes the client
func (c *Client) Close() error {
	return c.metricClient.Close()
}

// QueryTimeSeries queries time series data
func (c *Client) QueryTimeSeries(ctx context.Context, params QueryTimeSeriesParams) (*QueryTimeSeriesResult, error) {
	// Parse time range
	startTime, endTime, err := parseTimeRange(params.TimeRange)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time range: %w", err)
	}

	// Set defaults
	alignmentPeriod := params.AlignmentPeriodSec
	if alignmentPeriod <= 0 {
		alignmentPeriod = 60
	}

	maxSeries := params.MaxSeries
	if maxSeries <= 0 {
		maxSeries = 20
	}
	if maxSeries > 50 {
		maxSeries = 50
	}

	// Build filter
	filter := fmt.Sprintf(`metric.type = "%s"`, params.MetricType)
	if params.ResourceType != "" {
		filter += fmt.Sprintf(` AND resource.type = "%s"`, params.ResourceType)
	}
	for k, v := range params.Filters {
		filter += fmt.Sprintf(` AND %s = "%s"`, k, v)
	}

	// Create request
	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", params.ProjectID),
		Filter: filter,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(startTime),
			EndTime:   timestamppb.New(endTime),
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:  durationpb.New(time.Duration(alignmentPeriod) * time.Second),
			PerSeriesAligner: monitoringpb.Aggregation_ALIGN_MEAN,
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}

	// Execute query
	it := c.metricClient.ListTimeSeries(ctx, req)

	series := []TimeSeries{}
	totalPoints := 0

	for {
		ts, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate time series: %w", err)
		}

		points := []DataPoint{}
		for _, p := range ts.GetPoints() {
			value := extractValue(p.GetValue())
			points = append(points, DataPoint{
				Time:  p.GetInterval().GetEndTime().AsTime().Format(time.RFC3339),
				Value: value,
			})
		}

		series = append(series, TimeSeries{
			Metric: MetricLabels{
				Type:   ts.GetMetric().GetType(),
				Labels: ts.GetMetric().GetLabels(),
			},
			Resource: ResourceLabels{
				Type:   ts.GetResource().GetType(),
				Labels: ts.GetResource().GetLabels(),
			},
			Points: points,
		})

		totalPoints += len(points)

		if len(series) >= maxSeries {
			break
		}
	}

	return &QueryTimeSeriesResult{
		QueryMeta: QueryMeta{
			ProjectID:  params.ProjectID,
			MetricType: params.MetricType,
			Start:      startTime.Format(time.RFC3339),
			End:        endTime.Format(time.RFC3339),
		},
		Series: series,
		Stats: ResultStats{
			SeriesCount:     len(series),
			PointCountTotal: totalPoints,
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

func extractValue(v *monitoringpb.TypedValue) float64 {
	switch v := v.GetValue().(type) {
	case *monitoringpb.TypedValue_Int64Value:
		return float64(v.Int64Value)
	case *monitoringpb.TypedValue_DoubleValue:
		return v.DoubleValue
	case *monitoringpb.TypedValue_BoolValue:
		if v.BoolValue {
			return 1
		}
		return 0
	default:
		return 0
	}
}

// QueryTimeSeriesHandler returns a handler for the monitoring.query_time_series tool
func (c *Client) QueryTimeSeriesHandler() func(ctx context.Context, args json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var params QueryTimeSeriesParams
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("failed to parse arguments: %w", err)
		}

		if params.ProjectID == "" {
			return nil, fmt.Errorf("project_id is required")
		}
		if params.MetricType == "" {
			return nil, fmt.Errorf("metric_type is required")
		}

		return c.QueryTimeSeries(ctx, params)
	}
}
