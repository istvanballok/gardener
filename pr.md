How to categorize this PR?

/area monitoring
/kind enhancement

What this PR does / why we need it:

This PR merges the CoreDNS and Node Local DNS dashboards into a single improved DNS dashboard. The motivation that triggered this change was that with the CoreDNS version 1.11, the forward plugin deprecated some of the metrics used in the DNS upstream panels and hence those panels showed no data. Node Local DNS is a thin wrapper around CoreDNS and it still uses CoreDNS 1.10 internally (see [source](https://github.com/kubernetes/dns/blob/1.23.1/go.mod#L7). Hence, the upstream panels were not broken on the Node Local DNS dashboard.

The first commits in the history fix the broken DNS upstream panels of the CoreDNS dashboard:

    Adapt the shoot Prometheus scrape configuration to stop scraping the old deprecated metrics in favour of the new ones.
    Fix the CoreDNS upstream dashboards to query the new metrics.
    Revisit the wording used in the NodeLocalDNS dashboard, which referenced CoreDNS terminology.

The remaining commits in the history merge the CoreDNS and Node Local DNS dashboards into a single improved DNS dashboard.

While working on this we noticed that many of the panels could be improved. For instance, some of the old queries used the type label from the CoreDNS metrics although honor_labels (default false) is not set for the CoreDNS scrape configuration. This means that this label collides with the Gardener type label (that has values shoot or seed) and the original CoreDNS type label is renamed to exported_type. Other queries used counter values without the rate: they calculated the difference between the current value and the value $__interval minutes ago. $__interval is a variable managed by Grafana and is set depending on the time range selected. Its value is oftentimes one minute and depending on the resolution, there might not be any values from one minute ago. *Prometheus looks back by default to the preceding five minutes, so I think this can not happen. Rather, this approach does not handle counter resets.* Hence, calculating the aforementioned difference could yield bogus results. Instead, Grafana provides $__rate_interval, which guarantees at least two values when using the rate function. Using the rate is anyhow a good idea *so I would mention that it is idiomatic to use the rate function for counter values* for counters because it is resilient to counter restarts (e.g., if a pod is restarted). Calculating the difference between counters leads to a negative number when the counter restarts. All in all, not using the rate for counter metrics leads to unexpected visualizations.

The CoreDNS and Node Local DNS dashboards are now merged into a single dashboard to make it easier to maintain these dashboards. They share most of the panels and, when not, the panels in the new single dashboard clearly indicate so. An additional benefit of having a single dashboard is also the unified view of all DNS activity within a shoot, regardless of CoreDNS or Node Local DNS. We introduced a drop-down button to choose the DNS job: CoreDNS, Node Local DNS or both.

This PR also introduces a new hack utility to support renovating dashboards. We used it here to create minimal commits that can be easy to review: we do a small change in the Plutono UI, save the dashboard and export it into the codebase using this tool.

Finally, the following screenshots compare the status of the previous and new dashboards:
Previous CoreDNS dashboard
Previous Node Local DNS dashboard
New DNS dashboard here the area fill is missing

Special notes for your reviewer:

/cc @istvanballok @rickardsjp
/fyi @adenitiu @etiennnr

Release note:

Merge the CoreDNS and Node Local DNS dashboards into a single improved DNS dashboard


+ the pod names are not sorted alphabetically in the drop-down of the dashboard variable
