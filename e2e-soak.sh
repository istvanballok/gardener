#!/usr/bin/env bash
# e2e-soak.sh - run the HA multi-zone e2e suite N times to assess reliability
#
# Usage: ./e2e-soak.sh [RUNS] [SLEEP_SECONDS]
#   RUNS              number of iterations          (default: 10)
#   SLEEP_SECONDS     pause between runs in seconds (default: 600 = 10 min)
#
# Environment:
#   PARALLEL_E2E_TESTS  ginkgo --procs value (default: 5)
#
# Output: output-01.log .. output-N.log + summary printed at the end

RUNS="${1:-10}"
SLEEP_BETWEEN="${2:-600}"
export PARALLEL_E2E_TESTS="${PARALLEL_E2E_TESTS:-5}"

declare -a results=()

for i in $(seq 1 "$RUNS"); do
  logfile=$(printf 'output-%02d.log' "$i")
  printf '\n=== Run %d/%d  start: %s  log: %s ===\n' \
    "$i" "$RUNS" "$(date '+%Y-%m-%d %H:%M:%S')" "$logfile"

  unbuffer bash -c '
    GOFLAGS="-tags=musl" make kind-multi-zone-down || true
    GOFLAGS="-tags=musl" make import-tools-bin ci-e2e-kind-ha-multi-zone
  ' |& ts | nl | tee "$logfile"
  rc=${PIPESTATUS[0]}

  if (( rc == 0 )); then
    results+=("PASS")
    printf '=== Run %d PASSED ===\n' "$i"
  else
    results+=("FAIL(rc=$rc)")
    printf '=== Run %d FAILED (rc=%d) ===\n' "$i" "$rc"
  fi

  if (( i < RUNS )); then
    printf 'Sleeping %d min before run %d...  %s\n' \
      "$((SLEEP_BETWEEN / 60))" "$((i + 1))" "$(date '+%H:%M:%S')"
    sleep "$SLEEP_BETWEEN"
  fi
done

printf '\n=== Summary ===\n'
pass=0; fail=0
for i in $(seq 1 "$RUNS"); do
  r="${results[$((i-1))]}"
  printf 'Run %02d: %s\n' "$i" "$r"
  [[ "$r" == "PASS" ]] && (( pass++ )) || (( fail++ ))
done
printf 'Total: %d/%d passed\n' "$pass" "$RUNS"
