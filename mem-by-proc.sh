#!/usr/bin/env bash
# mem-by-proc.sh - aggregate RSS and private memory usage by executable name
#
# Uses a shell loop to read /proc/<pid>/statm and /proc/<pid>/comm directly,
# avoiding awk FILENAME parsing issues on Alpine/busybox awk.
#
# Columns:
#   RSS_MiB  - total resident physical RAM (includes shared pages)
#   SHR_MiB  - shared portion of RSS (shared libs, file mappings)
#   PRIV_MiB - private anonymous memory (RSS - SHR): heap + stack
#   N        - number of processes with this executable name
#   COMMAND  - executable name (from /proc/<pid>/comm, max 15 chars)
#
# Usage:
#   ./mem-by-proc.sh              one-shot snapshot, sorted by RSS
#   watch -n 5 ./mem-by-proc.sh   refresh every 5 seconds

PAGE_KiB=4  # 4 KiB pages on x86_64

{
  printf '%8s %8s %8s %4s  %s\n' RSS_MiB SHR_MiB PRIV_MiB N COMMAND

  for statm in /proc/[0-9]*/statm; do
    [[ -r "$statm" ]] || continue
    pid="${statm%/statm}"
    pid="${pid##*/}"
    # fields: size rss shr text lib data dirty
    read -r _ rss shr _ 2>/dev/null < "$statm" || continue
    name=$(cat "/proc/$pid/comm" 2>/dev/null) || continue
    printf '%d\t%d\t%s\n' "$((rss * PAGE_KiB))" "$((shr * PAGE_KiB))" "$name"
  done | awk -F'\t' '
    {
      rss[$3] += $1
      shr[$3] += $2
      cnt[$3]++
    }
    END {
      for (name in rss) {
        priv = rss[name] - shr[name]
        printf "%8d %8d %8d %4d  %s\n",
          rss[name]/1024, shr[name]/1024, priv/1024, cnt[name], name
      }
    }
  ' | sort -k1 -rn
}
