#!/usr/bin/env bash
# 删除 peer-wan 相关日志，供 cron 使用（每天 5:00）。
set -euo pipefail

LOG_DIRS=(
  "/var/log/peer-wan"
  "/var/log/peer-wan-agent"
)

for d in "${LOG_DIRS[@]}"; do
  if [ -d "$d" ]; then
    find "$d" -type f -mtime +0 -print -delete
  fi
done
