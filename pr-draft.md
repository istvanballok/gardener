TITLE: Prometheus recording-rule improvements related to metering

**How to categorize this PR?**
/area metering
/kind regression
/kind bug

**What this PR does / why we need it**:

This PR bundles three independent improvements to the metering recording rules. Per-commit details and rationale are in the respective commit messages; in short:

1. Project `garden_shoot_info` to only the labels that are relevant for metering, mitigating a performance regression caused by a recent label addition in gardener-metrics-exporter ([PR 145](https://github.com/gardener/gardener-metrics-exporter/pull/145)).
2. Unify and increase the `last_over_time()` window in the metering rules to 30m.
3. Drop a bogus `or sum_over_time` fallback in the cache / aggregate `:avg_over_time` recording rules.

**Which issue(s) this PR fixes**:

Fixes #

**Special notes for your reviewer**:

The changes have been verified using the remote local setup.

cc @vicwicker

**Release note**:

```bugfix operator
Improve robustness, accuracy, and resource consumption of the Prometheus recording rules related to metering.
```
