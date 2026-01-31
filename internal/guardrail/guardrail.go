package guardrail

import (
	"fmt"
	"time"

	"github.com/kaz-under-the-bridge/google-cloud-ops-mcp/internal/config"
)

// Guardrail はクエリのガードレールを実装
type Guardrail struct {
	cfg *config.Config
}

// New は新しいGuardrailを作成
func New(cfg *config.Config) *Guardrail {
	return &Guardrail{cfg: cfg}
}

// ValidateProjectID はプロジェクトIDが許可されているか検証
func (g *Guardrail) ValidateProjectID(projectID string) error {
	if !g.cfg.IsProjectAllowed(projectID) {
		return fmt.Errorf("project_id '%s' is not in the allowed list", projectID)
	}
	return nil
}

// ValidateTimeRange は時間範囲が制限内か検証
func (g *Guardrail) ValidateTimeRange(start, end time.Time) error {
	duration := end.Sub(start)
	maxDuration := time.Duration(g.cfg.Limits.MaxRangeHours) * time.Hour

	if duration > maxDuration {
		return fmt.Errorf("time range %.1f hours exceeds maximum %d hours",
			duration.Hours(), g.cfg.Limits.MaxRangeHours)
	}

	if duration < 0 {
		return fmt.Errorf("invalid time range: start time is after end time")
	}

	return nil
}

// ClampLogLimit はログ件数を制限内に収める
func (g *Guardrail) ClampLogLimit(limit int) int {
	if limit <= 0 {
		return 200 // デフォルト
	}
	if limit > g.cfg.Limits.MaxLogEntries {
		return g.cfg.Limits.MaxLogEntries
	}
	return limit
}

// ClampTimeSeriesLimit は時系列数を制限内に収める
func (g *Guardrail) ClampTimeSeriesLimit(limit int) int {
	if limit <= 0 {
		return 20 // デフォルト
	}
	if limit > g.cfg.Limits.MaxTimeSeries {
		return g.cfg.Limits.MaxTimeSeries
	}
	return limit
}

// Config は設定を返す（読み取り専用）
func (g *Guardrail) Config() *config.Config {
	return g.cfg
}
