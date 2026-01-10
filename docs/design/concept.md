# mcp-gcp-ops 設計コンセプト

## 目的

AI（Claude Code等）から **GCP Cloud Logging / Cloud Monitoring を安全に検索・集約・要約**できるようにする。

## 方針

**thin wrapper**（= API呼び出しと最小整形のみ）。推論・分析はAIに寄せる。

---

## ゴール / 非ゴール

### ゴール

- Cloud Logging のログ検索を **構造化してAIに渡す**
- Cloud Monitoring のメトリクス検索（時系列取得）を **構造化してAIに渡す**
- 「障害初動で必要な絞り込み」を **tool interface として固定化**する
- 権限・コスト・範囲を制御できる（安全で再現性がある）

### 非ゴール

- 相関分析・根因推定・改善提案をサーバ側でやらない（AIに任せる）
- ダッシュボード生成やSLO判定など高度機能は後回し
- 全API網羅はしない（まずは "現場で使う最小"）

---

## アーキテクチャ（薄い wrapper の原則）

### MCP Server（このプロジェクト）

- **認証**: ADC（Application Default Credentials）を使用
- **実体**: GCP Logging API / Monitoring API を呼ぶだけ
- **出力**: JSON（AIが読みやすい構造）
- **通信**: stdio ベースの JSON-RPC

### クライアント（Claude Code / Cursor）

- tool を呼び出して raw data を取得
- AIがフィルタ追加・要約・仮説生成を行う

### Go実装の理由

- **バイナリ1本で配布可能**
- **低レイテンシ・堅牢性・並列処理**が優れている
- **GCP SDK が強力**: `cloud.google.com/go/logging/logadmin`, `cloud.google.com/go/monitoring/apiv3`
- **認証が自然**: `google.golang.org/api/option` で ADC/SA を扱える
- **ガードレール実装がやりやすい**: allowlist / range / limit の管理

---

## 認証・権限（安全設計）

### 認証方式（優先順）

1. **Workload Identity / SA**（CI/サーバ運用）
2. **ADC**（ローカル開発：`gcloud auth application-default login`）

### IAM最小権限

#### Logging
- `roles/logging.viewer`

#### Monitoring
- `roles/monitoring.viewer`

#### プロジェクト一覧（必要なら）
- `roles/browser`（避けられるなら不要）

### ガードレール（MCP側）

#### allowlist
```yaml
allowed_project_ids: [...]
allowed_resource_types: [...]  # 任意
```

#### クエリ制限
- **最大期間**: `max_range_hours: 72`
- **最大返却件数**: `max_log_entries: 500`
- **最大系列数**: `max_time_series: 50`

#### 料金/負荷対策
- 時間範囲のデフォルトを短く（例：30分）
- サンプリング対応

---

## Tool設計（MCP interface）

最小で "現場で回る" 4ツールを定義する。

### 3.1 logging.query

**目的**: Logs Explorer相当の検索をAIが安全に実行できるようにする。

#### 入力

```typescript
{
  project_id: string,
  filter: string,  // Logging Query Language
  time_range: {
    start: string,  // RFC3339 or relative ("-1h")
    end: string     // RFC3339 or "now"
  },
  order: "desc" | "asc",  // default: desc
  limit: number,          // default: 200, max: 500
  fields?: string[],      // optional: 返すフィールドを絞る
  summarize?: boolean     // optional: server側集計ON/OFF（基本OFF推奨）
}
```

#### 出力

```typescript
{
  query_meta: {
    project_id: string,
    start: string,
    end: string,
    filter: string,
    limit: number
  },
  entries: [
    {
      timestamp: string,
      severity: string,
      log_name: string,
      resource: {
        type: string,
        labels: {...}
      },
      labels: {...},
      trace?: string,
      span_id?: string,
      http_request?: {...},
      text_payload?: string,
      json_payload?: object,
      proto_payload?: object,
      insert_id: string
    }
  ],
  stats: {
    returned_count: number,
    sampled: boolean,
    next_page_token?: string
  }
}
```

#### 注意

- 返す payload は "丸ごと" だと巨大化するので `fields` 指定推奨
- trace/span があれば必ず返す（相関の鍵）

---

### 3.2 logging.top_errors

**目的**: 初動で欲しい "エラー上位" をワンショットで取る。

#### 入力

```typescript
{
  project_id: string,
  time_range: {...},
  group_by: string,  // "service" | "resource.type" | "log_name" | "jsonPayload.exception" など
  filter_extra?: string,
  limit_groups: number,      // default 20
  sample_per_group: number   // default 3
}
```

#### 出力

```typescript
{
  groups: [
    {
      key: string,
      count: number,
      sample_entries: [...]  // logging entry subset
    }
  ]
}
```

#### 実装メモ

- Logging API には "集計クエリ" が弱いので client側でグルーピングする thin集計でOK
- ただし返却件数上限は厳守

---

### 3.3 monitoring.query_time_series

**目的**: メトリクスを時系列で取得してAIに渡す。

#### 入力

```typescript
{
  project_id: string,
  metric_type: string,     // e.g. "run.googleapis.com/request_count"
  resource_type: string,   // e.g. "cloud_run_revision"
  filters?: {              // label: value
    [key: string]: string
  },
  alignment: {
    alignment_period_sec: number,       // default 60
    per_series_aligner?: string,        // 例: RATE, MEAN, MAX
    cross_series_reducer?: string,      // 例: SUM, MEAN
    group_by_fields?: string[]
  },
  time_range: {...},
  max_series: number  // default 20
}
```

#### 出力

```typescript
{
  query_meta: {...},
  series: [
    {
      metric: {...},
      resource: {...},
      points: [
        {
          time: string,
          value: number
        }
      ]
    }
  ],
  stats: {
    series_count: number,
    point_count_total: number
  }
}
```

---

### 3.4 monitoring.list_metric_descriptors

**目的**: AIが "どのmetric_typeを使うべきか" を探索するためのツール。

#### 入力

```typescript
{
  project_id: string,
  prefix?: string,
  filter?: string,
  limit: number  // default 200
}
```

#### 出力

```typescript
{
  descriptors: [
    {
      type: string,
      metric_kind: string,
      value_type: string,
      unit: string,
      description: string,
      labels: {...}
    }
  ]
}
```

---

## "AIが回る" 運用シナリオ（具体）

### 障害初動（Cloud Run想定）

1. `logging.top_errors`（直近30分、serviceでgroup）
2. 最上位groupを `logging.query` で深掘り（trace付き）
3. `monitoring.query_time_series` で以下を同一レンジで取得：
   - `request_count`
   - `error_count` / 5xx
   - `latency`
4. AIに「仮説→検証→次のクエリ」を回させる

### デプロイ後監視

- "deploy timestamp" を入力
- 前後30分で以下をAIに比較させる：
  - error増加
  - latency悪化
  - 特定revisionの偏り

---

## 実装方針（Go）

### 重要なポイント

- **入力スキーマの固定**: tool の引数を明確に定義
- **ガードレール**: allowlist / range / limit を確実に実装
- **出力の安定**: JSON構造を固定

### 推奨事項

- **ローカル**: ADC
- **本番**: SA + allowlist
- **ログ payload**: デフォルトで "必要最小" にする

### Go実装での注意点

#### 1. MCPの実装形態

- MCP serverは stdio（JSON-RPC）で通信
- Goで実装する場合：
  - stdin/stdoutでJSON-RPC処理
  - tool schema管理（名前・引数・返却）を自前で整える

#### 2. GCP API SDK

- **Logging**: `cloud.google.com/go/logging/logadmin`
- **Monitoring**: `cloud.google.com/go/monitoring/apiv3`
- **認証**: `google.golang.org/api/option` で ADC/SA を自然に扱える

#### 3. ガードレール実装

- allowlist / range / limit
- 返却サイズ制限
- "危険なフィルタ"ブロック（例：期間が長すぎる等）

---

## MVP の範囲（最小で出す）

- **Tools**: `logging.query` / `monitoring.query_time_series`
- **Guardrails**: project allowlist + max_range_hours + max_limit
- **Output**: JSON固定
- **Docs**: 使い方例（Cloud Run / GKE）

---

## 次フェーズ（後回し）

- Trace / Error Reporting 追加
- SLO/SLIツール化
- Incident要約テンプレ自動生成
- Project v2同期（ダッシュボード）※別件
