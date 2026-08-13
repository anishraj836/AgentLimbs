#!/usr/bin/env bash
set -euo pipefail

echo "=================================================="
echo "      CRAWLER MONOREPO ROBUSTNESS VERIFICATION   "
echo "=================================================="

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "[1/4] Running Project Build across all packages..."
go build ./...

echo "[2/4] Running go vet across all packages..."
go vet ./...

echo "[3/4] Running go test -v across all packages..."
go test -v ./...

echo "[4/4] Running go test -race across all packages..."
go test -race ./...

echo "=================================================="
echo "    VERIFICATION COMPLETE: ZERO RACES & FAILURES  "
echo "=================================================="
