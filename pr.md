Improve the cache Prometheus for seeds with many shoots

**How to categorize this PR?**
/area monitoring
/kind enhancement

**What this PR does / why we need it**:
In seeds with many shoots, we observed that the cache Prometheus in the garden namespace of the seed was restarted by the kubelet again and again because of failing readiness and liveness probes. This PR improves the cache Prometheus configuration and the related scrape configuration in the shoot control plane Prometheus to prevent this issue, such that the cache Prometheus should stay healthy even in seeds with 250+ running shoots.

**Which issue(s) this PR fixes**:
Fixes an issue that the cache Prometheus was constantly restarted in seeds with 250+ shoots.

**Special notes for your reviewer**:

/cc

**Release note**:
```other operator
Improve the cache Prometheus configuration for seeds with many shoots
```
