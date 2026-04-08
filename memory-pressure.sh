#!/usr/bin/env bash
# memory-pressure.sh - monitor memory pressure and thrashing indicators
#
# Usage: ./memory-pressure.sh |& ts | nl
#   ts  (moreutils) prepends a timestamp to each line
#   nl  adds line numbers for easy scrollback reference

HEADER_EVERY=10  # reprint column headers every N data lines

print_header() {
  printf '%-9s %8s %9s %10s %10s %10s %10s %8s %7s %s\n' \
    TIME 'FREE_GiB' 'AVAIL_GiB' 'MAJFLT/m' 'PGSCAN_k/m' 'PGSCAN_d/m' 'STEAL_d/m' 'STALL_s' 'EFF_d%' 'LOAD 1m 5m 15m'
}

{
  DISK=$(awk '$3~/^(sd[a-z]|vd[a-z]|nvme[0-9]+n[0-9]+)$/{print $3;exit}' /proc/diskstats)

  prev_mf=$(awk '/^pgmajfault /{print $2}'     /proc/vmstat)
  prev_pk=$(awk '/^pgscan_kswapd /{print $2}'  /proc/vmstat)
  prev_pd=$(awk '/^pgscan_direct /{print $2}'  /proc/vmstat)
  prev_sd=$(awk '/^pgsteal_direct /{print $2}' /proc/vmstat)
  prev_rdc=$(awk -v d="$DISK" '$3==d{print $4}' /proc/diskstats)
  prev_rdt=$(awk -v d="$DISK" '$3==d{print $7}' /proc/diskstats)

  line=0
  while true; do
    if (( line % HEADER_EVERY == 0 )); then
      print_header
    fi

    sleep 60

    cur_mf=$(awk '/^pgmajfault /{print $2}'     /proc/vmstat)
    cur_pk=$(awk '/^pgscan_kswapd /{print $2}'  /proc/vmstat)
    cur_pd=$(awk '/^pgscan_direct /{print $2}'  /proc/vmstat)
    cur_sd=$(awk '/^pgsteal_direct /{print $2}' /proc/vmstat)
    cur_rdc=$(awk -v d="$DISK" '$3==d{print $4}' /proc/diskstats)
    cur_rdt=$(awk -v d="$DISK" '$3==d{print $7}' /proc/diskstats)

    dmf=$((cur_mf  - prev_mf))
    dpd=$((cur_pd  - prev_pd))
    dsd=$((cur_sd  - prev_sd))
    drdc=$((cur_rdc - prev_rdc))
    drdt=$((cur_rdt - prev_rdt))

    # if/else avoids division by zero when no disk reads occurred in the interval
    stall=$(awk -v dmf=$dmf -v drdc=$drdc -v drdt=$drdt \
      'BEGIN{ if (drdc>0) printf "%.1f", dmf*(drdt/drdc)/1000; else printf "n/a" }')
    eff=$(awk -v dpd=$dpd -v dsd=$dsd \
      'BEGIN{ if (dpd>0) printf "%.1f%%", dsd/dpd*100; else printf "n/a" }')

    printf '%-9s %8.1f %9.1f %10d %10d %10d %10d %8s %7s %s\n' \
      "$(date +%H:%M:%S)" \
      "$(awk '/^MemFree:/{printf "%.1f",$2/1048576}'      /proc/meminfo)" \
      "$(awk '/^MemAvailable:/{printf "%.1f",$2/1048576}' /proc/meminfo)" \
      "$dmf" "$((cur_pk-prev_pk))" "$dpd" "$dsd" \
      "$stall" "$eff" \
      "$(cut -d' ' -f1-3 /proc/loadavg)"

    prev_mf=$cur_mf; prev_pk=$cur_pk; prev_pd=$cur_pd; prev_sd=$cur_sd
    prev_rdc=$cur_rdc; prev_rdt=$cur_rdt
    (( line++ )) || true
  done
} | tee memory-pressure.log
