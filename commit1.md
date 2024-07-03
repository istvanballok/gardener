Extract dependencies with commented versions to their own require block

The next commits will use use the new dependency kube-state-metrics, and
`go mod tidy` will convert it to a direct one.

Because of controller-manager dependency with a commented version,
`go mod tidy` will identify the new kube-state-metrics dependency
belongs to none of the existing require blocks and it will attempt to
create a new block just for kube-state-metrics. Note that the default
behaviour of go.mod is two keep two blocks: one for direct dependencies
and one for indirect.

This commit extracts this third type of dependency (dependency with
commented version) into its own require block. This way, running
`go mod tidy` in the following commits will preserve the three blocks
defined.
