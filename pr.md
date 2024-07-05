How to categorize this PR?

/area monitoring
/kind enhancement

What this PR does / why we need it:

This PR merges the CoreDNS and Node Local DNS dashboards into a single improved DNS dashboard. The motivation that triggered this change was that with the CoreDNS version 1.11, the forward plugin deprecated some of the metrics used in the DNS upstream panels and hence those panels showed no data. Node Local DNS is a thin wrapper around CoreDNS and it still uses CoreDNS 1.10 internally. Hence, only the CoreDNS dashboard needed to be fixed. The first commits in the history fix the broken DNS upstream panels of the CoreDNS dashboard:

    Adapt the shoot Prometheus scrape configuration to stop scraping the old deprecated metrics in favour of the new ones.
    Fix the CoreDNS upstream dashboards to query the new metrics.
    Revisit the wording used in the NodeLocalDNS dashboard, which referenced CoreDNS terminology.

The remaining commits in the history merge the CoreDNS and Node Local DNS dashboards into a single improved DNS dashboard.

While working on this we noticed that many of the panels could be improved. For instance, some of the old queries used the type label from the CoreDNS metrics although honor_labels (default false) is not set for the CoreDNS scrape configuration. This means that this label collides with the Gardener type label (i.e., that has values shoot or seed) and the original CoreDNS type label is renamed to exported_type. Other queries used counter values without the rate function: they calculated the difference between the current value and the value $__interval minutes ago. Calculating the aforementioned difference returns bogus values when the counter is reset and the rate function should be used instead, which properly handles counter resets.

The CoreDNS and Node Local DNS dashboards are merged into a single dashboard to reduce duplication and ease maintenance. They share most of the panels and, when not, the panels in the new single dashboard clearly indicate so. An additional benefit of having a single dashboard is also the unified view of all DNS activity within a shoot, regardless of CoreDNS or Node Local DNS. We introduced a drop-down button to choose the DNS job: Core DNS, Node Local DNS or both (default).

This PR also introduces a new hack utility to support renovating dashboards. We used it here to create minimal commits that can be easy to review: we did a small change in the Plutono UI, saved the dashboard and exported it into the codebase using this tool.

Finally, the following screenshots compare the status of the previous and new dashboards:
Previous CoreDNS dashboard
Previous Node Local DNS dashboard
New DNS dashboard

Special notes for your reviewer:

/cc @istvanballok @rickardsjp
/fyi @adenitiu @etiennnr

Release note:

Merge the CoreDNS and Node Local DNS dashboards into a single improved DNS dashboard
