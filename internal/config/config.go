package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config はMCPサーバーの設定
type Config struct {
	AllowedProjectIDs []string `yaml:"allowed_project_ids"`
	Limits            Limits   `yaml:"limits"`
}

// Limits はクエリ制限の設定
type Limits struct {
	MaxRangeHours int `yaml:"max_range_hours"`
	MaxLogEntries int `yaml:"max_log_entries"`
	MaxTimeSeries int `yaml:"max_time_series"`
}

// DefaultConfig はデフォルト設定を返す
func DefaultConfig() *Config {
	return &Config{
		AllowedProjectIDs: []string{}, // 空 = 制限なし
		Limits: Limits{
			MaxRangeHours: 72,
			MaxLogEntries: 500,
			MaxTimeSeries: 50,
		},
	}
}

// Load は設定ファイルを読み込む
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 設定ファイルがなければデフォルト設定を使用
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// デフォルト値の補完
	if cfg.Limits.MaxRangeHours <= 0 {
		cfg.Limits.MaxRangeHours = 72
	}
	if cfg.Limits.MaxLogEntries <= 0 {
		cfg.Limits.MaxLogEntries = 500
	}
	if cfg.Limits.MaxTimeSeries <= 0 {
		cfg.Limits.MaxTimeSeries = 50
	}

	return cfg, nil
}

// IsProjectAllowed はプロジェクトIDが許可されているか確認
func (c *Config) IsProjectAllowed(projectID string) bool {
	// 許可リストが空の場合は全て許可
	if len(c.AllowedProjectIDs) == 0 {
		return true
	}

	for _, allowed := range c.AllowedProjectIDs {
		if allowed == projectID {
			return true
		}
	}
	return false
}
