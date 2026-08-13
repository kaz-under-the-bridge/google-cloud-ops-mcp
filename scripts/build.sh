#!/bin/bash
# Docker経由でビルド（Go環境不要）
set -e

cd "$(dirname "$0")/.."

# Goモジュールキャッシュ用ディレクトリ
GOCACHE_DIR="${HOME}/.cache/gcp-ops-mcp-build"
mkdir -p "${GOCACHE_DIR}/mod" "${GOCACHE_DIR}/cache"

echo "Building gcp-ops-mcp with Docker..."
docker run --rm \
  -v "$(pwd)":/app \
  -v "${GOCACHE_DIR}/mod":/go/pkg/mod \
  -v "${GOCACHE_DIR}/cache":/root/.cache/go-build \
  -w /app \
  -e GOOS=darwin \
  -e GOARCH=arm64 \
  golang:1.24 \
  go build -o gcp-ops-mcp .

echo "Done: ./gcp-ops-mcp"
