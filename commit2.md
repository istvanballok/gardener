    Adapt VPA metrics to new solution CustomResourceState

    This commit adds the necessary configuration changes to readd the VPA
    metrics using the new strategy CustomResourceState from the
    kube-state-metrics. Such configuration is created programatically thanks
    to the the kube-state-metrics dependency downloaded in the previous
    commit. This commit also runs `go mod tidy` in order to make this
    dependency direct.

    The new CustomResourceState configuration is stored in a ConfigMap and
    mounted into the kube-state-metrics deployment using the
    `--custom-resource-state-config-file`. There is an alternative command
    flag `--custom-resource-state-config` to pass such configuration inline
    but the resulting configuration is very lengthy, which would make the
    kube-state-metrics deployment document not so readable.

    We tried to preserve the old metric labels and naming as much as
    possible. However, there are few changes:

    1. We can't have one metric regardless the resource and have the resource
       type in the labels, but we need to migrate to one specific metric per
       resource. For instance, previously we would have a `target` metric
       with labels `resource=cpu` or `resource=memory` to indicate the value
       corresponded to CPU or memory. Instead, after this change two metrics
       are added: `target_cpu` and `target_memory`. Nevertheless, this can
       be seen as an improvement as the new metrics do not mix cores (i.e.,
       CPU) and bytes (i.e., memory) in their values anymore.
    2. Using CustomResourceState prefixes the metric name with
       `kube_customresource` by default. Therefore, the old prefix
       `kube_verticalpodautoscaler` is moved to
       `kube_customresource_verticalpodautoscaler`. Such default value can
       be overwritten to match the prefix we want, but we choose not to so
       that the new name reflects better that the CustomResourceState is
       used.
    3. Property `nilIsZero` is set to true for the recommendation metrics to
       set value to zero if the recommendation path does not exist in the
       VPA spec file (i.e., it doesn't have a recommendation). In the past,
       the time series would simply not exist. Since a recommendation to 0
       does not make sense, this is a way, e.g., in the dashboard, to know
       there is no recommendation, rather than relying on not having data,
       which might also be the case if there is an issue and metrics are not
       generated.
    4. Metrics containing `container_policies` in their name have been
       renamed to `containerpolicies` to respect the containerPolicies key
       in the spec file (similar to resourcePolicy or
       containerRecommendations). This way, the metric format is unified to
       use underscores to separate keys in the path.

    A unit test is added to assert the generated configuration matches the
    one originally generated for testing. The test expectation is saved into
    a yaml file and comitted it to git. The test saves the generated
    configuration in a different file in the temporary folder (but does not
    commit it). The benefit of this approach is that it helps comparing the
    actual and expected configuration, no matter how large the files are: we
    have two separate files that we can compare using any tool.

    The `expectedCustomResourceStateConfig()` utility returns the expected
    CustomResourceState config and also asserts that the actual value is the
    same. This function is to be used to load the test expectation during
    the test setup. The assertion is performed inside this function to allow
    to give more human readable errors when the long config document actually
    differs. When the assertion fails, a custom message shows. It tries to
    mimic the wording from usual ginkgo test runs but adds a hint in the end
    on how to use the diff command to see the diff. This of course will only
    work on UNIX systems. Developers can review this diff and react
    accordingly e.g., check if there is a bug in the code or if indeed the
    expectation has to be changed.

    Finally, this commit also runs `go mod tidy` to make the new
    kube-state-metrics dependency direct.
