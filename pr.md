How to categorize this PR?

/area monitoring
/kind enhancement

What this PR does / why we need it:

This PR merges the CoreDNS and Node Local DNS dashboards into a single improved DNS dashboard. The motivation that triggered this change was that the CoreDNS version 1.11 forward plugin deprecated some of metrics used in the DNS upstream panels and broke them. Node Local DNS is a thin wrapper around CoreDNS and it still uses CoreDNS 1.10 internally. Hence, only the CoreDNS dashboard needed to be fixed. The first commits in the history:

    Adapt the shoot Prometheus scrape configuration to stop scraping the old deprecated metrics in favour of the new ones.
    Fix the CoreDNS upstream dashboards to query the new metrics.
    Revisit the wording used in the NodeLocalDNS dashboard, which referenced CoreDNS terminology.

However, while working on this we noticed that many of the panels could be improved. For instance, some of the old queries would use the type label from the CoreDNS metrics despite honor_labels (default false) is not set for the CoreDNS scrape configuration. This means this label collisions with the Gardener type label (i.e., values shoot or seed) and the original CoreDNS type label is renamed to exported_type. Other queries would use counter values without the rate: they would calculate the difference between the current value and the value $__interval minutes ago. $__interval is a variable managed by Grafana and is set depending on the time range selected. Its value is oftentimes one minute and depending on the resolution, there might not be any values from one minute ago. Hence, calculating the aforementioned difference could turn bogus. Instead, Grafana provides $__rate_interval, which guarantees at least two values when using the rate function. Using the rate is anyhow a good idea for counters because it is resilient to counter restarts (e.g., if a pod is restarted). Calculating the difference between counters might lead to negative numbers if the counters are restarted. All in all, not using the rate for counter metrics leads to unexpected visualizations.

The CoreDNS and Node Local DNS dashboard are merged into one dashboard to ease fixing duplicated bugs. They share most of the panels and, when not, the panels in the new single dashboard clearly indicate so. An additional benefit of having a single dashboard is also the unified view of all DNS activity within a shoot, regardless of CoreDNS or Node Local DNS. We introduced a drop-down button to choose specific DNS jobs.

This PR also introduces a new hack utility to iterate over dashboard changes. We used it here to achieve minimal commits that can be easy to review: we do a small change in the Plutono UI, save the dashboard and export it into the codebase using this tool.

Finally, the following screenshots compare the status of the previous and new dashboards:
Previous CoreDNS dashboard
Previous Node Local DNS dashboard
New DNS dashboard

Special notes for your reviewer:

/cc @istvanballok @rickardsjp
/fyi @adenitiu @etiennnr

Release note:

Merge CoreDNS and Node Local DNS dashboard into a single improved DNS dashboard
