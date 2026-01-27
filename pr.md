# Introduce the ObservabilityComponentsHealthy condition to the Seeds

**How to categorize this PR?**
/area monitoring
/kind enhancement

**What this PR does / why we need it**:

This PR introduces a new condition `ObservabilityComponentsHealthy` on the `Seed` resource to report the health status of the observability components deployed in the garden namespace of the seed cluster.

Previously, the health status of these components was reported via the `SeedSystemComponentsHealthy` condition, which includes other Gardener components as well.

This change follows a similar concept like the dedicated `ObservabilityComponentsHealthy` condition on the `Shoot` resource, introduced with #7325.

The observability components include Prometheus, Alertmanager, Plutono, Vali, fluentbit and kube-state-metrics. The new condition reflects the status of the respective managed resources and the result of the recently introduced (#13341) Prometheus health checks.

With this change, both the shoot and the seed resources have an `ObservabilityComponentsHealthy` condition. For managed seeds, the shoot conditions controller in the Gardener controller manager copies the conditions of the seed to the shoot to make them easily accessible in the Gardener dashboard. To avoid conflicts, the seed's `ObservabilityComponentsHealthy` condition is prefixed with `Seed` when copied to the shoot resource.

**Which issue(s) this PR fixes**:
Fixes https://github.com/gardener/gardener/pull/13341#discussion_r2708931291

**Special notes for your reviewer**:
cc @vicwicker

The changes in `pkg/gardenlet/controller/seed/care/health_test.go` are split into multiple commits for easier review.

**Release note**:

```noteworthy operator
The status of the observability components in a seed cluster is reported to the new `ObservabilityComponentsHealthy` condition on the `Seed` resource, instead of the common `SeedSystemComponentsHealthy` condition. For managed seeds, the condition is prefixed with `Seed` when it is copied from the seed resource to the shoot resource.
```

```noteworthy operator
TODO: A condition threshold for the new `ObservabilityComponentsHealthy` condition can be configured in Gardener installations.
```
