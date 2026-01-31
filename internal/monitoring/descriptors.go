package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
)

// ListMetricDescriptorsParams are the parameters for monitoring.list_metric_descriptors
type ListMetricDescriptorsParams struct {
	ProjectID string `json:"project_id"`
	Filter    string `json:"filter"` // Optional filter (e.g., "metric.type = starts_with(\"run.googleapis.com\")")
	Limit     int    `json:"limit"`  // Maximum number of descriptors to return
}

// ListMetricDescriptorsResult is the result of monitoring.list_metric_descriptors
type ListMetricDescriptorsResult struct {
	QueryMeta   DescriptorsQueryMeta `json:"query_meta"`
	Descriptors []MetricDescriptor   `json:"descriptors"`
	Stats       DescriptorsStats     `json:"stats"`
}

type DescriptorsQueryMeta struct {
	ProjectID string `json:"project_id"`
	Filter    string `json:"filter,omitempty"`
}

type MetricDescriptor struct {
	Type        string  `json:"type"`
	DisplayName string  `json:"display_name"`
	Description string  `json:"description"`
	MetricKind  string  `json:"metric_kind"`
	ValueType   string  `json:"value_type"`
	Unit        string  `json:"unit,omitempty"`
	Labels      []Label `json:"labels,omitempty"`
}

type Label struct {
	Key         string `json:"key"`
	ValueType   string `json:"value_type"`
	Description string `json:"description,omitempty"`
}

type DescriptorsStats struct {
	ReturnedCount int  `json:"returned_count"`
	Truncated     bool `json:"truncated"`
}

// ListMetricDescriptors lists available metric descriptors
func (c *Client) ListMetricDescriptors(ctx context.Context, params ListMetricDescriptorsParams) (*ListMetricDescriptorsResult, error) {
	// Set defaults
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	// Create request
	req := &monitoringpb.ListMetricDescriptorsRequest{
		Name:   fmt.Sprintf("projects/%s", params.ProjectID),
		Filter: params.Filter,
	}

	// Execute query
	it := c.metricClient.ListMetricDescriptors(ctx, req)

	descriptors := []MetricDescriptor{}
	truncated := false

	for {
		desc, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate metric descriptors: %w", err)
		}

		labels := make([]Label, len(desc.GetLabels()))
		for i, l := range desc.GetLabels() {
			labels[i] = Label{
				Key:         l.GetKey(),
				ValueType:   l.GetValueType().String(),
				Description: l.GetDescription(),
			}
		}

		descriptors = append(descriptors, MetricDescriptor{
			Type:        desc.GetType(),
			DisplayName: desc.GetDisplayName(),
			Description: desc.GetDescription(),
			MetricKind:  desc.GetMetricKind().String(),
			ValueType:   desc.GetValueType().String(),
			Unit:        desc.GetUnit(),
			Labels:      labels,
		})

		if len(descriptors) >= limit {
			truncated = true
			break
		}
	}

	return &ListMetricDescriptorsResult{
		QueryMeta: DescriptorsQueryMeta{
			ProjectID: params.ProjectID,
			Filter:    params.Filter,
		},
		Descriptors: descriptors,
		Stats: DescriptorsStats{
			ReturnedCount: len(descriptors),
			Truncated:     truncated,
		},
	}, nil
}

// ListMetricDescriptorsHandler returns a handler for the monitoring.list_metric_descriptors tool
func (c *Client) ListMetricDescriptorsHandler() func(ctx context.Context, args json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var params ListMetricDescriptorsParams
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("failed to parse arguments: %w", err)
		}

		if params.ProjectID == "" {
			return nil, fmt.Errorf("project_id is required")
		}

		return c.ListMetricDescriptors(ctx, params)
	}
}

// ListMetricDescriptorsHandlerWithGuardrail returns a handler with guardrail validation
func (c *Client) ListMetricDescriptorsHandlerWithGuardrail(v Validator) func(ctx context.Context, args json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var params ListMetricDescriptorsParams
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

		return c.ListMetricDescriptors(ctx, params)
	}
}

// NewMetricServiceClient creates a new MetricService client for listing descriptors
// Note: The existing Client already has MetricClient which can list descriptors
func NewMetricServiceClient(ctx context.Context) (*monitoring.MetricClient, error) {
	return monitoring.NewMetricClient(ctx)
}

// Helper function to build common filters
func BuildMetricFilter(prefix string) string {
	if prefix == "" {
		return ""
	}
	// Escape the prefix for filter syntax
	prefix = strings.ReplaceAll(prefix, `"`, `\"`)
	return fmt.Sprintf(`metric.type = starts_with("%s")`, prefix)
}
