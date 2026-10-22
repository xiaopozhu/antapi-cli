#!/usr/bin/env bash
# 入口只构建固定源码制品，发布阶段复用已验证的候选目录。
set -euo pipefail
cd "$(dirname "$0")/.."
exec python3 scripts/build-release.py "$@"
