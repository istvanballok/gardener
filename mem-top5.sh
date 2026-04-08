#!/usr/bin/env bash
# mem-top5.sh - print top 5 processes by RSS every minute for scrollback history
#
# Usage: ./mem-top5.sh |& ts | nl

INTERVAL=60
SCRIPT_DIR="$(dirname "$0")"

while true; do
  printf '=== %s ===\n' "$(date +%H:%M:%S)"
  "$SCRIPT_DIR/mem-by-proc.sh" | head -6   # header + 5 data rows
  sleep "$INTERVAL"
done
