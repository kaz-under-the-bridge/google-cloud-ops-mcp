# google-cloud-ops-mcp

GCP Cloud Logging / Monitoring を AI（Claude Code等）から安全に使うための薄い MCP サーバー。

## 概要

このプロジェクトは、AIエージェントが GCP の運用情報にアクセスできるようにする **thin wrapper** として動作します。
- ログ検索（Cloud Logging）
- メトリクス取得（Cloud Monitoring）
- 障害初動での絞り込みを tool interface として提供

推論・分析はAIに任せ、APIコールと最小限の整形のみを行います。

## 特徴

- **安全性**: Allowlist、時間範囲制限、件数制限などのガードレールを実装
- **シンプル**: 複雑な分析はせず、構造化データをAIに渡すのみ
- **Go実装**: 単一バイナリで配布可能、低レイテンシ
- **ADC対応**: Application Default Credentials で認証

## 必要な権限

最小限のIAM権限：
- `roles/logging.viewer` - ログ読み取り
- `roles/monitoring.viewer` - メトリクス読み取り

## インストール

```bash
go install github.com/kaz-under-the-bridge/google-cloud-ops-mcp@latest
```

または:

```bash
git clone https://github.com/kaz-under-the-bridge/google-cloud-ops-mcp.git
cd google-cloud-ops-mcp
go build -o gcp-ops-mcp
```

## 設定

認証方式（優先順）：
1. Workload Identity / Service Account（本番環境）
2. ADC（開発環境）：`gcloud auth application-default login`

設定ファイル例（`config.yaml`）：

```yaml
allowed_project_ids:
  - my-project-id
  - another-project-id

limits:
  max_range_hours: 72
  max_log_entries: 500
  max_time_series: 50
```

## MCP Tools

提供される主要なツール：

### `logging.query`
Logs Explorer 相当の検索

### `logging.top_errors`
エラーの上位を集計して取得（初動調査用）

### `monitoring.query_time_series`
メトリクスの時系列データを取得

### `monitoring.list_metric_descriptors`
利用可能なメトリクスを探索

詳細は [docs/design/concept.md](docs/design/concept.md) を参照。

## 使用例

### Claude Code / Cursor での設定

MCP設定ファイル（`~/.cursor/mcp.json` など）に追加：

```json
{
  "mcpServers": {
    "gcp-ops": {
      "command": "/path/to/gcp-ops-mcp",
      "args": ["-config", "/path/to/config.yaml"]
    }
  }
}
```

### 典型的な障害調査フロー

1. `logging.top_errors` で直近30分のエラー上位を確認
2. 気になるエラーを `logging.query` で深掘り（trace付き）
3. `monitoring.query_time_series` で関連メトリクス（request_count, latency等）を取得
4. AIに「仮説→検証→次のクエリ」を回させる

## 開発

```bash
# 依存関係のインストール
go mod download

# ビルド
go build -o gcp-ops-mcp

# テスト
go test ./...

# ローカル実行（stdio mode）
./gcp-ops-mcp -config config.yaml
```

## アーキテクチャ

- **通信方式**: stdio ベースの JSON-RPC
- **GCP SDK**: 
  - `cloud.google.com/go/logging/logadmin`
  - `cloud.google.com/go/monitoring/apiv3`
- **認証**: `google.golang.org/api/option` で ADC/SA を扱う

詳細設計は [docs/design/concept.md](docs/design/concept.md) を参照。

## MVP 範囲

- [x] Tools: `logging.query` / `monitoring.query_time_series`
- [x] Guardrails: project allowlist + 時間範囲制限 + 件数制限
- [x] Output: JSON固定
- [x] Docs: 使い方例

## 今後の拡張（後回し）

- Trace / Error Reporting 追加
- SLO/SLI ツール化
- Incident 要約テンプレート自動生成

## ライセンス

MIT License

## 参考リンク

- [MCP (Model Context Protocol)](https://modelcontextprotocol.io/)
- [GCP Cloud Logging API](https://cloud.google.com/logging/docs/reference/v2/rest)
- [GCP Cloud Monitoring API](https://cloud.google.com/monitoring/api/v3)
